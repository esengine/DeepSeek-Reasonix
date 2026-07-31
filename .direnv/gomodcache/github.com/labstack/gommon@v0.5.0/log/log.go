package log

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/valyala/fasttemplate"

	"github.com/labstack/gommon/color"
)

type (
	Logger struct {
		prefix     string
		level      uint32
		skip       int
		output     io.Writer
		template   *fasttemplate.Template
		levels     []string
		color      *color.Color
		bufferPool sync.Pool
		mutex      sync.Mutex
	}

	Lvl uint8

	JSON map[string]interface{}
)

const (
	DEBUG Lvl = iota + 1
	INFO
	WARN
	ERROR
	OFF
	panicLevel
	fatalLevel
)

var (
	global        = New("-")
	defaultHeader = `{"time":"${time_rfc3339_nano}","level":"${level}","prefix":"${prefix}",` +
		`"file":"${short_file}","line":"${line}"}`
)

func init() {
	global.skip = 3
}

func New(prefix string) (l *Logger) {
	l = &Logger{
		level:    uint32(INFO),
		skip:     2,
		prefix:   prefix,
		template: l.newTemplate(defaultHeader),
		color:    color.New(),
		bufferPool: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 256))
			},
		},
	}
	l.initLevels()
	l.SetOutput(output())
	return
}

func (l *Logger) initLevels() {
	l.levels = []string{
		"-",
		l.color.Blue("DEBUG"),
		l.color.Green("INFO"),
		l.color.Yellow("WARN"),
		l.color.Red("ERROR"),
		"",
		l.color.Yellow("PANIC", color.U),
		l.color.Red("FATAL", color.U),
	}
}

func (l *Logger) newTemplate(format string) *fasttemplate.Template {
	return fasttemplate.New(format, "${", "}")
}

func (l *Logger) DisableColor() {
	l.color.Disable()
	l.initLevels()
}

func (l *Logger) EnableColor() {
	l.color.Enable()
	l.initLevels()
}

func (l *Logger) Prefix() string {
	return l.prefix
}

func (l *Logger) SetPrefix(p string) {
	l.prefix = p
}

func (l *Logger) Level() Lvl {
	return Lvl(atomic.LoadUint32(&l.level))
}

func (l *Logger) SetLevel(level Lvl) {
	atomic.StoreUint32(&l.level, uint32(level))
}

func (l *Logger) Output() io.Writer {
	return l.output
}

func (l *Logger) SetOutput(w io.Writer) {
	l.output = w
	if w, ok := w.(*os.File); !ok || !isatty.IsTerminal(w.Fd()) {
		l.DisableColor()
	}
}

func (l *Logger) Color() *color.Color {
	return l.color
}

func (l *Logger) SetHeader(h string) {
	l.template = l.newTemplate(h)
}

func (l *Logger) Print(i ...interface{}) {
	l.log(0, fmt.Sprint(i...), false)
}

func (l *Logger) Printf(format string, args ...interface{}) {
	l.log(0, fmt.Sprintf(format, args...), false)
}

func (l *Logger) Printj(j JSON) {
	l.logJSON(0, j)
}

func (l *Logger) Debug(i ...interface{}) {
	l.log(DEBUG, fmt.Sprint(i...), false)
}

func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(DEBUG, fmt.Sprintf(format, args...), false)
}

func (l *Logger) Debugj(j JSON) {
	l.logJSON(DEBUG, j)
}

func (l *Logger) Info(i ...interface{}) {
	l.log(INFO, fmt.Sprint(i...), false)
}

func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(INFO, fmt.Sprintf(format, args...), false)
}

func (l *Logger) Infoj(j JSON) {
	l.logJSON(INFO, j)
}

func (l *Logger) Warn(i ...interface{}) {
	l.log(WARN, fmt.Sprint(i...), false)
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(WARN, fmt.Sprintf(format, args...), false)
}

func (l *Logger) Warnj(j JSON) {
	l.logJSON(WARN, j)
}

func (l *Logger) Error(i ...interface{}) {
	l.log(ERROR, fmt.Sprint(i...), false)
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(ERROR, fmt.Sprintf(format, args...), false)
}

func (l *Logger) Errorj(j JSON) {
	l.logJSON(ERROR, j)
}

func (l *Logger) Fatal(i ...interface{}) {
	l.log(fatalLevel, fmt.Sprint(i...), false)
	os.Exit(1)
}

func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.log(fatalLevel, fmt.Sprintf(format, args...), false)
	os.Exit(1)
}

func (l *Logger) Fatalj(j JSON) {
	l.logJSON(fatalLevel, j)
	os.Exit(1)
}

func (l *Logger) Panic(i ...interface{}) {
	msg := fmt.Sprint(i...)
	l.log(panicLevel, msg, false)
	panic(msg)
}

func (l *Logger) Panicf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.log(panicLevel, msg, false)
	panic(msg)
}

func (l *Logger) Panicj(j JSON) {
	l.logJSON(panicLevel, j)
	panic(j)
}

func DisableColor() {
	global.DisableColor()
}

func EnableColor() {
	global.EnableColor()
}

func Prefix() string {
	return global.Prefix()
}

func SetPrefix(p string) {
	global.SetPrefix(p)
}

func Level() Lvl {
	return global.Level()
}

func SetLevel(level Lvl) {
	global.SetLevel(level)
}

func Output() io.Writer {
	return global.Output()
}

func SetOutput(w io.Writer) {
	global.SetOutput(w)
}

func SetHeader(h string) {
	global.SetHeader(h)
}

func Print(i ...interface{}) {
	global.Print(i...)
}

func Printf(format string, args ...interface{}) {
	global.Printf(format, args...)
}

func Printj(j JSON) {
	global.Printj(j)
}

func Debug(i ...interface{}) {
	global.Debug(i...)
}

func Debugf(format string, args ...interface{}) {
	global.Debugf(format, args...)
}

func Debugj(j JSON) {
	global.Debugj(j)
}

func Info(i ...interface{}) {
	global.Info(i...)
}

func Infof(format string, args ...interface{}) {
	global.Infof(format, args...)
}

func Infoj(j JSON) {
	global.Infoj(j)
}

func Warn(i ...interface{}) {
	global.Warn(i...)
}

func Warnf(format string, args ...interface{}) {
	global.Warnf(format, args...)
}

func Warnj(j JSON) {
	global.Warnj(j)
}

func Error(i ...interface{}) {
	global.Error(i...)
}

func Errorf(format string, args ...interface{}) {
	global.Errorf(format, args...)
}

func Errorj(j JSON) {
	global.Errorj(j)
}

func Fatal(i ...interface{}) {
	global.Fatal(i...)
}

func Fatalf(format string, args ...interface{}) {
	global.Fatalf(format, args...)
}

func Fatalj(j JSON) {
	global.Fatalj(j)
}

func Panic(i ...interface{}) {
	global.Panic(i...)
}

func Panicf(format string, args ...interface{}) {
	global.Panicf(format, args...)
}

func Panicj(j JSON) {
	global.Panicj(j)
}

func (l *Logger) logJSON(level Lvl, j JSON) {
	b, err := json.Marshal(j)
	if err != nil {
		panic(err)
	}
	l.log(level, string(b), true)
}

func (l *Logger) log(level Lvl, message string, jsonBody bool) {
	if level < l.Level() && level != 0 {
		return
	}
	buf := l.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer l.bufferPool.Put(buf)
	// JSON callers route through an extra logJSON wrapper; account for
	// that frame so runtime.Caller still lands on the user's code. Keep
	// this in sync with logJSON — if the wrapper is ever inlined away
	// or moved, drop the increment.
	skip := l.skip
	if jsonBody {
		skip++
	}
	_, file, line, _ := runtime.Caller(skip)

	_, err := l.template.ExecuteFunc(buf, func(w io.Writer, tag string) (int, error) {
		switch tag {
		case "time_rfc3339":
			return w.Write([]byte(time.Now().Format(time.RFC3339)))
		case "time_rfc3339_nano":
			return w.Write([]byte(time.Now().Format(time.RFC3339Nano)))
		case "level":
			return w.Write([]byte(l.levels[level]))
		case "prefix":
			return w.Write([]byte(l.prefix))
		case "long_file":
			return w.Write([]byte(file))
		case "short_file":
			return w.Write([]byte(path.Base(file)))
		case "line":
			return w.Write([]byte(strconv.Itoa(line)))
		}
		return 0, nil
	})
	if err != nil {
		return
	}

	s := buf.String()
	i := buf.Len() - 1
	if i >= 0 && s[i] == '}' {
		// JSON header
		buf.Truncate(i)
		buf.WriteByte(',')
		if jsonBody {
			buf.WriteString(message[1:])
		} else {
			buf.WriteString(`"message":`)
			buf.WriteString(strconv.Quote(message))
			buf.WriteString(`}`)
		}
	} else {
		// Text header
		if len(s) > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(message)
	}
	buf.WriteByte('\n')
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.output.Write(buf.Bytes())
}
