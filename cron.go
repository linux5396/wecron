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

//define a life style interface
type LifeStyle interface {
	//start a WeCron tab
	Start()
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

// Job is an interface for submitted WeCron jobs.
type Job interface {
	Run()
}

// The Schedule describes a job's duty cycle.
type Schedule interface {
	// Return the next activation time, later than the given time.
	// Next is invoked initially, and then each time the job is run.
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

	// The Job to run.
	Job Job
}

// TaskDispatcher is a wrapper for sorting the Task array by time
// (with zero time at the end).
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

//In Local zone.
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

// A wrapper that turns a func() into a WeCron.Job
type FuncJob func()

func (f FuncJob) Run() { f() }

// AddFunc adds a func to the WeCron to be run on the given schedule.
func (c *WeCron) AddFunc(spec string, cmd func()) error {
	return c.addJob(spec, FuncJob(cmd))
}

// AddJob adds a Job to the WeCron to be run on the given schedule.
func (c *WeCron) addJob(spec string, cmd Job) error {
	schedule, err := Parse(spec)
	if err != nil {
		return err
	}
	c.schedule(schedule, cmd)
	return nil
}

// Schedule adds a Job to the WeCron to be run on the given schedule.
func (c *WeCron) schedule(schedule Schedule, cmd Job) {
	Task := &Task{
		Schedule: schedule,
		Job:      cmd,
	}
	if c.state != Running {
		c.tasks = append(c.tasks, Task)
		return
	}
	c.newTask <- Task
}

// Location gets the time zone location
func (c *WeCron) Location() *time.Location {
	return c.location
}

// Start the WeCron scheduler in its own go-routine, or no-op if already started.
func (c *WeCron) Start() {
	if c.state == Running {
		return
	}
	//make it running by atomic
	atomic.CompareAndSwapInt64(&c.state, Ready, Running)
	go c.run()
}

// Run the WeCron scheduler, or no-op if already running.
func (c *WeCron) Run() {
	if c.state == Running {
		return
	}
	atomic.CompareAndSwapInt64(&c.state, Ready, Running)
	c.run()
}

func (c *WeCron) runWithRecovery(j Job) {
	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
		}
	}()
	j.Run()
}

// Run the scheduler. this is private just due to the need to synchronize
// access to the 'running' state variable.
func (c *WeCron) run() {
	// Figure out the next activation times for each Task.
	now := c.now()
	for _, Task := range c.tasks {
		Task.Next = Task.Schedule.Next(now)
	}

	for {
		// Sort the tasks every round.
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
					go c.runWithRecovery(e.Job)
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

//stops the WeCron scheduler if it is running; otherwise it does nothing.
func (c *WeCron) Destroy() {
	if c.state != Running {
		return
	}
	c.stop <- struct{}{}
	atomic.CompareAndSwapInt64(&c.state, Running, Stop)
}

//return current time in cron's location
func (c *WeCron) now() time.Time {
	return time.Now().In(c.location)
}
