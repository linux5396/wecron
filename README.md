# wecron

## Introduction
This is a cron tool only for testing.
I don't suggest that using this tool in a distributed system with multi nodes. 
Because that can not promise global single. By the way, although this tool can not be used in a distributed system, there are 
some special case below:
- Distributed cron task system  runs some scheduling tasks by using this as
  the basement support.

- High currency scheduling testing system. etc.

## usage
`
A cron expression represents a set of times, using 6 space-separated fields.
| Field name   | Mandatory? | Allowed values  | Allowed special characters |
| ------------ | ---------- | --------------- | -------------------------- |
| Seconds      | Yes        | 0-59            | * / , -                    |
| Minutes      | Yes        | 0-59            | * / , -                    |
| Hours        | Yes        | 0-23            | * / , -                    |
| Day of month | Yes        | 1-31            | * / , - ?                  |
| Month        | Yes        | 1-12            | * / , -                    |
| Day of week  | Yes        | 0-6             | * / , - ?                  |

---
```go
func TestWeCron_(t *testing.T) {
	cron := New()
	//支持多个任务
	//Every second 0 to 15 run
	cron.InQueue("0-15 * * * * *", func() {
		log.Print(" ")
	})
	//every second 45-59 run
	cron.InQueue("45-59 * * * * *", func() {
		log.Print(" ")
	})
	cron.InQueue("* * * 1 * *", func() {
		log.Print(" every first of month")
	})
	cron.StartAsync()
	//test add job in running state
	cron.InQueue("30-40 * * * * *", func() {
		log.Print(" testing")
	})
	//call destroy to stop
	//cron.Destroy()
	select {}
}
```

