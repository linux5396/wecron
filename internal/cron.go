package internal

import (
	"runtime"
	"sort"
	"sync/atomic"
	"time"
)

//definite 3 const state
const (
	Running int64 = iota
	Ready
	Stop
)

//define a life style interface
type LifeStyle interface {
	//init a WeCron tab
	Init()
	//destroy a WeCron tab
	Destroy()
}

// WeCron keeps track of any number of tasks, invoking the associated func as
// specified by the schedule. It may be started, stopped, and the tasks may
// be inspected while running.
type WeCron struct {
	tasks    []*Task       //some task
	stop     chan struct{} //a chan control stop
	newTask  chan *Task    //a thread safe promise. when adding a new task into  we cron , I use this chan *Task to sync task.
	state    int64         //a state change by atomic
	location *time.Location
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
	// Two zero times should return false.
	// Otherwise, zero is "greater" than any other time.
	// (To sort it at the end of the list.)
	if s[i].Next.IsZero() {
		return false
	}
	if s[j].Next.IsZero() {
		return true
	}
	return s[i].Next.Before(s[j].Next)
}

// New returns a new WeCron job runner, in the Local time zone.
func New() *WeCron {
	return NewWithLocation(time.Now().Location())
}

// NewWithLocation returns a new WeCron job runner.
func NewWithLocation(location *time.Location) *WeCron {
	return &WeCron{
		tasks:   nil,
		newTask: make(chan *Task),
		stop:    make(chan struct{}),
		//snapshot: make(chan []*Task),
		state:    Ready, //init ready state
		location: location,
	}
}

// A wrapper that turns a func() into a WeCron.Job
type FuncJob func()

func (f FuncJob) Run() { f() }

// AddFunc adds a func to the WeCron to be run on the given schedule.
func (c *WeCron) AddFunc(spec string, cmd func()) error {
	return c.AddJob(spec, FuncJob(cmd))
}

// AddJob adds a Job to the WeCron to be run on the given schedule.
func (c *WeCron) AddJob(spec string, cmd Job) error {
	schedule, err := Parse(spec)
	if err != nil {
		return err
	}
	c.Schedule(schedule, cmd)
	return nil
}

// Schedule adds a Job to the WeCron to be run on the given schedule.
func (c *WeCron) Schedule(schedule Schedule, cmd Job) {
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
		// Determine the next Task to run.
		sort.Sort(TaskDispatcher(c.tasks))

		var timer *time.Timer
		if len(c.tasks) == 0 || c.tasks[0].Next.IsZero() {
			// If there are no tasks yet, just sleep - it still handles new tasks
			// and stop requests.
			timer = time.NewTimer(100000 * time.Hour)
		} else {
			timer = time.NewTimer(c.tasks[0].Next.Sub(now))
		}

		for {
			select {
			case now = <-timer.C:
				now = now.In(c.location)
				// Run every Task whose next time was less than now
				for _, e := range c.tasks {
					if e.Next.After(now) || e.Next.IsZero() {
						break
					}
					go c.runWithRecovery(e.Job)
					e.Prev = e.Next
					e.Next = e.Schedule.Next(now)
				}

			case newTask := <-c.newTask:
				timer.Stop()
				now = c.now()
				newTask.Next = newTask.Schedule.Next(now)
				c.tasks = append(c.tasks, newTask)

			case <-c.stop:
				timer.Stop()
				return
			}

			break
		}
	}
}

// Stop stops the WeCron scheduler if it is running; otherwise it does nothing.
func (c *WeCron) Stop() {
	if c.state != Running {
		return
	}
	c.stop <- struct{}{}
	atomic.CompareAndSwapInt64(&c.state, Running, Stop)
}

// TaskSnapshot returns a copy of the current WeCron Task list.
func (c *WeCron) TaskSnapshot() []*Task {
	tasks := []*Task{}
	for _, e := range c.tasks {
		tasks = append(tasks, &Task{
			Schedule: e.Schedule,
			Next:     e.Next,
			Prev:     e.Prev,
			Job:      e.Job,
		})
	}
	return tasks
}

// now returns current time in c location
func (c *WeCron) now() time.Time {
	return time.Now().In(c.location)
}
