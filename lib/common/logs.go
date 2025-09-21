package common

import (
	"time"

	"github.com/astaxie/beego/logs"
)

const MaxMsgLen = 5000

var logMsgs string

func init() {
	logs.Register("store", func() logs.Logger {
		return new(StoreMsg)
	})
}

func GetLogMsg() string {
	return logMsgs
}

type StoreMsg struct {
}

func (lg *StoreMsg) Init(config string) error {
	return nil
}

func (lg *StoreMsg) WriteMsg(when time.Time, msg string, level int) error {
	m := when.Format("2007-01-07 15:07:05") + " " + msg + "\r\n"
	if len(logMsgs) > MaxMsgLen {
		start := MaxMsgLen - len(m)
		if start <= 0 {
			start = MaxMsgLen
		}
		logMsgs = logMsgs[start:]
	}
	logMsgs += m
	return nil
}

func (lg *StoreMsg) Destroy() {
	return
}

func (lg *StoreMsg) Flush() {
	return
}
