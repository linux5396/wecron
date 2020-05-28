package wecron

import (
	"runtime"
	"sort"
	"sync/atomic"
	"time"
)

//definite 3 const state
//ready ->running -> stop
const (
	Running int64 = iota
	Ready
	Stop
)

//define a life cycle interface
//TODO next version I want to support Suspend and Resume
type LifeCycle interface {
	//start a WeCron tab in sync
	StartSync()
	//start a WeCron tab in async
	StartAsync()
	//destroy a WeCron tab
	Destroy()
	//Suspend the we cron ,this version does not support it.
	//Suspend()
	//Resume the suspend we cron ,this version does not support it.
	//Resume()
}

// WeCron keeps track of any number of tasks, invoking the associated func as
// specified by the schedule. It may be started, stopped, and the tasks may
// be inspected while running.
type WeCron struct {
	tasks    []*Task        //some task
	stop     chan struct{}  //a chan control stop
	newTask  chan *Task     //a thread safe promise. when adding a new task into  we cron , I use this chan *Task to sync task.
	state    int64          //a state change by atomic.
	location *time.Location //time location
}

// The schedule uses Next time to indicate the time period of work
type Schedule interface {
	// Return the next activation time, later than the given time.
	// Next is invoked initially, and then each time the task is run.
	Next(time.Time) time.Time
}

// Task consists of a schedule and the func to execute on that schedule.
type Task struct {
	// The schedule on which this job should be run.
	Schedule Schedule
	// The next time the job will run. This is the zero time if WeCron has not been
	// started or this Task's schedule is unsatisfiable
	Next time.Time
	// The last time this job was run. This is the zero time if the job has never
	// been run.
	Prev time.Time
	//run
	run func()
}

// TaskDispatcher is a wrapper for sorting the Task array by time
type TaskDispatcher []*Task

func (s TaskDispatcher) Len() int      { return len(s) }
func (s TaskDispatcher) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s TaskDispatcher) Less(i, j int) bool {
	if s[i].Next.IsZero() {
		return false
	}
	if s[j].Next.IsZero() {
		return true
	}
	return s[i].Next.Before(s[j].Next)
}

//By current time zone
func New() *WeCron {
	return NewWithLocation(time.Now().Location())
}

// In a specified zone.
func NewWithLocation(location *time.Location) *WeCron {
	return &WeCron{
		tasks:    nil,
		newTask:  make(chan *Task),
		stop:     make(chan struct{}),
		state:    Ready, //init ready state
		location: location,
	}
}

//Add a timed task to the queue, when the instance is running, you can still continue to add
func (c *WeCron) InQueue(spec string, cmd func()) error {
	schedule, err := Parse(spec)
	if err != nil {
		return err
	}
	c.schedule(schedule, cmd)
	return nil
}

// add new job to the new task channel
//may wake up the run() 's routine and make a new round sorting.
func (c *WeCron) schedule(schedule Schedule, cmd func()) {
	Task := &Task{
		Schedule: schedule,
		run:      cmd,
	}
	if c.state != Running {
		c.tasks = append(c.tasks, Task)
		return
	}
	c.newTask <- Task
}

//Returns the time zone of the current instance
func (c *WeCron) Location() *time.Location {
	return c.location
}

//Use recovery mechanism to run tasks and avoid panic
func (c *WeCron) runWithRecovery(run func()) {
	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
		}
	}()
	run()
}

//The core operation method, the instance is in a running state
func (c *WeCron) run() {
	// Figure out the next activation times for each Task.
	now := c.now()
	for _, Task := range c.tasks {
		Task.Next = Task.Schedule.Next(now)
	}
	for {
		// Sort the tasks every round.
		//do you need to use time heap for optimization
		sort.Sort(TaskDispatcher(c.tasks))
		var timer *time.Timer
		//if there are no tasks, make it blocked till newTask channel recv task.
		if len(c.tasks) == 0 || c.tasks[0].Next.IsZero() {
			timer = time.NewTimer(time.Hour * 996)
		} else {
			//take a patience at this line, call the timer.C by the first
			timer = time.NewTimer(c.tasks[0].Next.Sub(now))
		}
		for {
			select {
			case now = <-timer.C:
				now = now.In(c.location)
				for _, e := range c.tasks {
					if e.Next.After(now) || e.Next.IsZero() {
						//because the tasks sorted early,so if a task is after now, the whole next tasks are after now.
						break
					}
					//run with a go routine to keep currency.
					go c.runWithRecovery(e.run)
					e.Prev = e.Next
					e.Next = e.Schedule.Next(now)
				}

			case newTask := <-c.newTask: //add new task
				timer.Stop()
				now = c.now()
				newTask.Next = newTask.Schedule.Next(now)
				c.tasks = append(c.tasks, newTask)

			case <-c.stop: //stop the world
				timer.Stop()
				return
			}

			break
		}
	}
}

//Start the instance asynchronously to avoid blocking
func (c *WeCron) StartAsync() {
	if c.state == Running {
		return
	}
	//make it running by atomic
	if atomic.CompareAndSwapInt64(&c.state, Ready, Running) {
		go c.run()
	} else {
		panic("wecron 'state is not readu , cannot invoke StartSync")
	}
}

// Start the instance synchronously, it will block
func (c *WeCron) StartSync() {
	if c.state == Running {
		return
	}
	if atomic.CompareAndSwapInt64(&c.state, Ready, Running) {
		c.run()
	} else {
		panic("wecron 'state is not readu , cannot invoke StartSync")
	}
}

// Destroy the instance to ensure that the next scheduled task will no longer be executed
//However, the scheduled task that is already being executed can only be handed over to continue execution until it is completed
func (c *WeCron) Destroy() {
	if c.state != Running {
		return
	}
	if atomic.CompareAndSwapInt64(&c.state, Running, Stop) {
		c.stop <- struct{}{}
		return
	}
	panic("wecron ' state is not running, cannot invoke Destroy.")
}

//Returns the current time in the time zone of the current instance
func (c *WeCron) now() time.Time {
	return time.Now().In(c.location)
}
