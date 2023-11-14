/*
equnix.asia/utils - package made by Equnix for any kind of utilites needed
---
Copyright 2022 Equnix Business Solutions, PT
All right reserved and it is forbidden to copy and use without any permission
*/
package utils

import (
	"fmt"
	"log/syslog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

/*
20220622
A new model of logging, use: L.INF, L.WRN, L.ERR, L.DBG without mentioning
Domain, and Context anymore, instead you can state the domain for all app scope
with: L.Dom = uint16(), and Context: L.Ctx = uint16
you may change the Context time to time to reflex to context of the current
process. If you wish not to use Dom, just don't set the L.Dom anyway
So it happen to Context (Ctx), you may use log with Ctx but without Dom, or
vice versa.

When L.Dom and L.Ctx is not set (=0) it means the field in log line is dissapear

*/

/*
Once utils is deployed, every submodules has its own init and will initialized
Log doesn't need to be instantiate manually, otherwise it will be spawned automatically
with L static var as the object.

LOG LEVEL:

	ERROR = iota
	The highest level of urgency of log

	WARNING
	When there a no harm issue, but it could potentially lead
	to a serious one.

	INFO
	Only for informational purpose

	DEBUG
	Debugging purpose only, there are 4 levels:
	DBG, DB1 DB2, DB3
	so by increasing or lowering the debug level (DEBUG0, DEBUG1...)
	we can control how verbose the output of the debug info
	remember: syslog cannot distinguish debug level, it will treated the same.

//from syslog

	LOG_EMERG Priority = iota
	LOG_ALERT
	LOG_CRIT
	LOG_ERR
	LOG_WARNING
	LOG_NOTICE
	LOG_INFO = 6
	LOG_DEBUG
*/
const (
	LOGGER_PREFIX = 1
	LOGGER_FNAME  = 2
	LOGGER_STDOUT = 3
	LOGGER_LEVEL  = 4

	LOG_LPREFIX = 1
	LOG_FNAME   = 2
	LOG_STDOUT  = 3
	LOG_LVEL    = 4
)

const (
	LOG_ERROR = iota
	LOG_WARNING
	LOG_INFO
	LOG_DEBUG
	LOG_VERBOSE
)

var (
	L            *Log
	LOG_PATH     string
	LOG_FILENAME string
	LOG_LEVEL    int

	LOG_PRIO syslog.Priority
)

type Log struct {
	slog *syslog.Writer
	flog *os.File

	Lchan   chan string
	run     bool
	dbuglvl int

	Sysinfo  string
	Priority syslog.Priority //Level of Log
	//stdout bool value of whether we want to copy into stdout
	Stdout,
	Syslog,
	Filelog bool

	rgfilter *regexp.Regexp

	Dom,
	Ctx uint16
}

/*
export LOGPARAM=DEBUG
*/
func init() {
	var err error

	//instantiate the object
	L = &Log{
		Priority: syslog.LOG_DEBUG,
		Lchan:    make(chan string, 1000),
		run:      true,
		// Stdout:   false,
		// Syslog:   false,
		// Filelog:  false,
		// dbuglvl: 3, //enhanced the level of debug
		Sysinfo: "EQUNIX",
	}

	//export LOGPARAM=SYSLOG, DEBUG, WARNING
	logparam := os.Getenv("LOGPARAM")
	for _, param := range strings.Split(logparam, ",") {
		switch strings.ToUpper(param) {
		case "DEBUG", "DEBUG0":
			L.Priority = syslog.LOG_DEBUG
			// L.dbuglvl = 0

		case "DEBUG1":
			L.Priority = syslog.LOG_DEBUG
			L.dbuglvl = 1

		case "DEBUG2":
			L.Priority = syslog.LOG_DEBUG
			L.dbuglvl = 2

		case "DEBUG3":
			L.Priority = syslog.LOG_DEBUG
			L.dbuglvl = 3

		case "DEBUG4":
			L.Priority = syslog.LOG_DEBUG
			L.dbuglvl = 4

		case "INFO":
			L.Priority = syslog.LOG_INFO

		case "WARNING":
			L.Priority = syslog.LOG_WARNING

		case "ERROR":
			L.Priority = syslog.LOG_ERR

		case "CRITICAL":
			L.Priority = syslog.LOG_CRIT

		case "ALERT": //the highest
			L.Priority = syslog.LOG_ALERT

		case "SYSLOG":
			L.slog, err = syslog.New(syslog.LOG_INFO, L.Sysinfo)

		case "FILE":
			err = L.fileOpen()
			//implicit initiate L.flog

		case "STDOUT":
			L.Stdout = true

		}
		if err != nil {
			fmt.Printf("Error in %s, logger is not run\n\n", param)
			L.run = false
		}

	}

	if L.run {
		go L.logger()
		<-L.Lchan //wait until the logger routine
	}
}

func (me *Log) fileOpen() (err error) {

	if me.flog != nil {
		err = me.flog.Close()
		if err != nil {
			err = fmt.Errorf("fileopen.close>%v", err)
			return
		}
	}
	me.flog, err = os.OpenFile(
		filepath.Base(os.Args[0])+"_"+
			time.Now().Format("2006-01-02")+".log",
		os.O_APPEND|os.O_WRONLY|os.O_CREATE,
		0600,
	)
	if err != nil {
		err = fmt.Errorf("fileopen>%v", err)
		return
	}

	return
}

// the daemon for handling logger
func (me *Log) logger() {
	var s string
	var err error

	runtime.LockOSThread()
	me.rgfilter = regexp.MustCompile(`\(\*([0-z_]+)\)\.([0-z_\(\)]+)$`)
	me.Lchan <- "logger is ok"
	cloop := 0
	_, _, oldday := time.Now().Date()
	for {
		s = <-me.Lchan
		if me.Syslog && me.slog == nil {
			L.slog, err = syslog.New(syslog.LOG_INFO, L.Sysinfo)
			if err != nil {
				fmt.Printf("Logger.init() New Syslog has error:%v\n"+
					"Syslog is not created.", err)
			}
		}

		if me.slog != nil && me.Syslog {
			_, err = me.slog.Write([]byte(s))
			if err != nil {
				fmt.Printf("syslog.write>%v\n", err)
			}
		}
		s = time.Now().Format("15:04:05.0000") + s

		if me.Stdout {
			fmt.Print(s)
		}

		if me.flog != nil && me.Filelog {
			cloop++
			if cloop >= 100 {
				cloop = 0
				_, _, newday := time.Now().Date()
				if oldday != newday {
					err = me.fileOpen()
					if err != nil {
						L.ERR(err, "Fail re-open file")
					}

				}
			}
			_, err = me.flog.Write([]byte(s))
			if err != nil {
				fmt.Printf("flog.write:%v\n", err)
			}
		}

	}

}

func (me *Log) Close() {
	for len(me.Lchan) > 0 {
		// fmt.Printf("waiting for logger close\n")
		time.Sleep(1 * time.Second)
	}

	close(me.Lchan)
}

// core function of log
func (me *Log) l(mindeep int, lvl byte, Dom, Ctx uint16, msg string) {
	var ifunc int
	var match []string
	var fn string
	var domcs string

	pc, _, line, ok := runtime.Caller(2 + mindeep)
	if ok {
		fn = runtime.FuncForPC(pc).Name()
		match = me.rgfilter.FindStringSubmatch(fn)
		if match != nil {
			ifunc = len(match) - 1
			if ifunc-1 < 0 {
				fn = match[ifunc]
			} else {
				fn = match[ifunc-1] + "." + match[ifunc]
			}
		}
	} else {
		fn = "<nf>"
	}

	if Dom == 0 {
		if Ctx == 0 {
			domcs = ""
		} else {
			domcs = fmt.Sprintf(":%04X|", Ctx)
		}
	} else {
		if Ctx == 0 {
			domcs = fmt.Sprintf("%04X:|", Dom)
		} else {
			domcs = fmt.Sprintf("%04X:%04X|", Dom, Ctx)
		}
	}

	msg = fmt.Sprintf("|%c|%s%s():%d %s\n", lvl, domcs, fn, line, msg)
	me.Lchan <- msg
}

func (me *Log) L(mindeep int, lvl byte, msg string) {
	var ifunc int
	var match []string
	var fn string
	var domcs string

	pc, _, line, ok := runtime.Caller(2 + mindeep)
	if ok {
		fn = runtime.FuncForPC(pc).Name()
		//`\(\*([0-z_]+)\)\.([0-z_\(\)]+)$`
		match = me.rgfilter.FindStringSubmatch(fn)
		if match != nil {
			ifunc = len(match) - 1
			if ifunc-1 < 0 {
				fn = match[ifunc]
			} else {
				fn = match[ifunc-1] + "." + match[ifunc]
			}
		}
	} else {
		fn = "<nf>"
	}

	if me.Dom == 0 {
		if me.Ctx == 0 {
			domcs = ""
		} else {
			domcs = fmt.Sprintf(":%04X|", me.Ctx)
		}
	} else {
		if me.Ctx == 0 {
			domcs = fmt.Sprintf("%04X|", me.Dom)
		} else {
			domcs = fmt.Sprintf("%04X:%04X|", me.Dom, me.Ctx)
		}

	}

	msg = fmt.Sprintf("|%c|%s%s():%d %s\n", lvl, domcs, fn, line, msg)
	//fmt.Print(msg)
	me.Lchan <- msg
}

func (me *Log) Debug(prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(0, 'D', 0xFFFF, 0xFFFF, prompt)
}

// this is special error logger where it did not require to input domain and csid
func (me *Log) Error(e interface{}, prompt string, v ...interface{}) {
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(1, 'E', 0xFFFF, 0xFFFF, me.errprn(e, prompt))
}

func (me *Log) errprn(e interface{}, prompt string) (msg string) {
	switch t := e.(type) {
	case byte, int:
		erid := e.(byte)
		msg = fmt.Sprintf("%s RC:%02d", prompt, erid)
	case error:
		err := e.(error)
		msg = fmt.Sprintf("%s err{%s}", prompt, err.Error())
	case string:
		ss := e.(string)
		msg = fmt.Sprintf("%s err{%s}", prompt, ss)
	default:
		msg = fmt.Sprintf("%s ???{type(%v)=%v}", prompt, t, e)
	}

	return
}

func (me *Log) Err(domid, csid uint16, e interface{}, prompt string, v ...interface{}) {

	if me.Priority < syslog.LOG_ERR {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}

	me.l(0, 'E', domid, csid, me.errprn(e, prompt))
}

func (me *Log) Warn(domid, csid uint16, prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_WARNING {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(0, 'W', domid, csid, prompt)
}

func (me *Log) Info(domid, csid uint16, prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_INFO {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(0, 'I', domid, csid, prompt)
}

func (me *Log) Dbg(domid, csid uint16, prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(0, 'D', domid, csid, prompt)
}

func (me *Log) Dbg1(domid, csid uint16, prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG && me.dbuglvl < 1 {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(0, '1', domid, csid, prompt)
}

func (me *Log) Dbg2(domid, csid uint16, prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG && me.dbuglvl < 2 {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(0, '2', domid, csid, prompt)
}

func (me *Log) Dbg3(domid, csid uint16, prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG && me.dbuglvl < 3 {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(0, '3', domid, csid, prompt)
}

func (me *Log) Dbg4(domid, csid uint16, prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG && me.dbuglvl < 4 {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.l(0, '4', domid, csid, prompt)
}

//
//******************************************************* new logger
//

func (me *Log) ERR(e interface{}, prompt string, v ...interface{}) {

	if me.Priority < syslog.LOG_ERR {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}

	me.L(0, 'E', me.errprn(e, prompt))
}

func (me *Log) WRN(prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_WARNING {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.L(0, 'W', prompt)
}

func (me *Log) INF(prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_INFO {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.L(0, 'I', prompt)
}

func (me *Log) DBG(prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.L(0, 'D', prompt)
}

func (me *Log) DB1(prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG && me.dbuglvl < 1 {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.L(0, '1', prompt)
}

func (me *Log) DB2(prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG && me.dbuglvl < 2 {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.L(0, '2', prompt)
}

func (me *Log) DB3(prompt string, v ...interface{}) {
	if me.Priority < syslog.LOG_DEBUG && me.dbuglvl < 3 {
		return
	}
	if v != nil {
		prompt = fmt.Sprintf(prompt, v...)
	}
	me.L(0, '3', prompt)
}

func (me *Log) Sync() {
	for len(me.Lchan) > 0 {
		time.Sleep(100 * time.Millisecond)
	}
}
