package wecron

import (
	"log"
	"testing"
)

func TestWeCron_AddFunc(t *testing.T) {
	cron := New()
	//支持多个任务
	//Every second 0 to 15 run
	cron.AddFunc("0-15 * * * * *", func() {
		log.Print(" ")
	})
	//every second 45-59 run
	cron.AddFunc("45-59 * * * * *", func() {
		log.Print(" ")
	})
	cron.AddFunc("* * * 1 * *", func() {
		log.Print(" every first of month")
	})
	go cron.Start()
	//test add job in running state
	cron.AddFunc("30-40 * * * * *", func() {
		log.Print(" testing")
	})
	//call destroy to stop
	//cron.Destroy()
	select {}
}
