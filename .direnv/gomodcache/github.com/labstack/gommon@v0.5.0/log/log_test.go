package log

import (
	"bytes"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func test(l *Logger, t *testing.T) {
	b := new(bytes.Buffer)
	l.SetOutput(b)
	l.DisableColor()
	l.SetLevel(WARN)

	l.Print("print")
	l.Printf("print%s", "f")
	l.Debug("debug")
	l.Debugf("debug%s", "f")
	l.Info("info")
	l.Infof("info%s", "f")
	l.Warn("warn")
	l.Warnf("warn%s", "f")
	l.Error("error")
	l.Errorf("error%s", "f")

	assert.Contains(t, b.String(), "print")
	assert.Contains(t, b.String(), "printf")
	assert.NotContains(t, b.String(), "debug")
	assert.NotContains(t, b.String(), "debugf")
	assert.NotContains(t, b.String(), "info")
	assert.NotContains(t, b.String(), "infof")
	assert.Contains(t, b.String(), `"level":"WARN","prefix":"`+l.prefix+`"`)
	assert.Contains(t, b.String(), `"message":"warn"`)
	assert.Contains(t, b.String(), `"level":"ERROR","prefix":"`+l.prefix+`"`)
	assert.Contains(t, b.String(), `"message":"errorf"`)
}

func TestLog(t *testing.T) {
	l := New("test")
	test(l, t)
}

func TestGlobal(t *testing.T) {
	test(global, t)
}

func TestLogConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			TestLog(t)
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestFatal(t *testing.T) {
	l := New("test")
	switch os.Getenv("TEST_LOGGER_FATAL") {
	case "fatal":
		l.Fatal("fatal")
		return
	case "fatalf":
		l.Fatalf("fatal-%s", "f")
		return
	}

	loggerFatalTest(t, "fatal", "fatal")
	loggerFatalTest(t, "fatalf", "fatal-f")
}

func loggerFatalTest(t *testing.T, env string, contains string) {
	buf := new(bytes.Buffer)
	cmd := exec.Command(os.Args[0], "-test.run=TestFatal")
	cmd.Env = append(os.Environ(), "TEST_LOGGER_FATAL="+env)
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		assert.Contains(t, buf.String(), contains)
		return
	}
	t.Fatalf("process ran with err %v, want exit status 1", err)
}

func TestNoFormat(t *testing.T) {
}

func TestFormat(t *testing.T) {
	l := New("test")
	l.SetHeader("${level} | ${prefix}")
	b := new(bytes.Buffer)
	l.SetOutput(b)
	l.Info("info")
	assert.Equal(t, "INFO | test info\n", b.String())
}

func TestJSON(t *testing.T) {
	l := New("test")
	b := new(bytes.Buffer)
	l.SetOutput(b)
	l.SetLevel(DEBUG)
	l.Debugj(JSON{"name": "value"})
	assert.Contains(t, b.String(), `"name":"value"`)
}

func TestStringWithQuotes(t *testing.T) {
	l := New("test")
	b := new(bytes.Buffer)
	l.SetOutput(b)
	l.SetLevel(DEBUG)
	l.Debugf("Content-Type: %q", "")
	assert.Contains(t, b.String(), `"message":"Content-Type: \"\""`)
}

func TestEmptyHeader(t *testing.T) {
	l := New("test")
	b := new(bytes.Buffer)
	l.SetOutput(b)
	l.SetHeader("")
	l.SetLevel(DEBUG)
	l.Debugf("captain's log")
	assert.Contains(t, b.String(), `captain's log`)
}

// TestCallerFile verifies that both text and JSON log paths report the
// user's call site, not an internal wrapper frame. This guards against
// skip-count drift when the core log method is refactored.
func TestCallerFile(t *testing.T) {
	l := New("test")
	l.SetHeader("${short_file}")
	b := new(bytes.Buffer)
	l.SetOutput(b)
	l.SetLevel(DEBUG)

	b.Reset()
	l.Info("text call")
	assert.Contains(t, b.String(), "log_test.go", "text path should report caller file")

	b.Reset()
	l.Infoj(JSON{"hello": "world"})
	assert.Contains(t, b.String(), "log_test.go", "JSON path should report caller file")
}

func BenchmarkLog(b *testing.B) {
	l := New("test")
	l.SetOutput(new(bytes.Buffer))
	for i := 0; i < b.N; i++ {
		l.Infof("Info=%s, Debug=%s", "info", "debug")
	}
}
