package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Event struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Phase   string    `json:"phase"`
	Message string    `json:"message"`
}
type Logger struct {
	file  *os.File
	Path  string
	Quiet bool
	mu    sync.Mutex
}

func NewLogger(dir, runID string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, runID+".jsonl")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f, Path: p}, nil
}
func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
func (l *Logger) Log(level, phase, msg string) {
	msg = Redact(msg)
	e := Event{Time: time.Now().UTC(), Level: level, Phase: phase, Message: msg}
	if l != nil && l.file != nil {
		l.mu.Lock()
		defer l.mu.Unlock()
		b, _ := json.Marshal(e)
		_, _ = l.file.Write(append(b, '\n'))
	}
	if l == nil || !l.Quiet {
		fmt.Printf("[%s] %s\n", phase, msg)
	}
}

type Result struct {
	ExitCode int
	Output   string
	Err      error
}

func Run(ctx context.Context, logger *Logger, phase, name string, args []string, env []string, onLine func(string)) Result {
	return run(ctx, logger, phase, name, args, env, onLine, nil)
}

// RunWithStdout runs a command with the same cancellation, process-group and
// logging guarantees as Run, while streaming stdout to the supplied writer.
// Stderr is still redacted, logged and returned in Result.Output.
func RunWithStdout(ctx context.Context, logger *Logger, phase, name string, args []string, env []string, onLine func(string), stdout io.Writer) Result {
	if stdout == nil {
		return Result{ExitCode: -1, Err: fmt.Errorf("stdout writer 不能为空")}
	}
	return run(ctx, logger, phase, name, args, env, onLine, stdout)
}

func run(ctx context.Context, logger *Logger, phase, name string, args []string, env []string, onLine func(string), stdoutWriter io.Writer) Result {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if err == syscall.ESRCH {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	var stdout io.Reader
	if stdoutWriter == nil {
		var err error
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return Result{ExitCode: -1, Err: err}
		}
	} else {
		cmd.Stdout = stdoutWriter
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{ExitCode: -1, Err: err}
	}
	if logger != nil {
		logger.Log("info", phase, "执行 "+safeCommand(name, args))
	}
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1, Err: err}
	}
	var lines []string
	var linesMu sync.Mutex
	var callbackMu sync.Mutex
	readerCount := 1
	if stdout != nil {
		readerCount++
	}
	done := make(chan struct{}, readerCount)
	read := func(r io.Reader, level string) {
		defer func() { done <- struct{}{} }()
		s := bufio.NewScanner(r)
		buf := make([]byte, 64*1024)
		s.Buffer(buf, 1024*1024)
		for s.Scan() {
			line := Redact(s.Text())
			linesMu.Lock()
			if len(lines) < 2000 {
				lines = append(lines, line)
			}
			linesMu.Unlock()
			if onLine != nil {
				callbackMu.Lock()
				onLine(line)
				callbackMu.Unlock()
			} else if logger != nil {
				logger.Log(level, phase, line)
			}
		}
	}
	if stdout != nil {
		go read(stdout, "info")
	}
	go read(stderr, "error")
	for i := 0; i < readerCount; i++ {
		<-done
	}
	err = cmd.Wait()
	code := 0
	if err != nil {
		code = -1
		var ee *exec.ExitError
		if ok := errorAs(err, &ee); ok {
			code = ee.ExitCode()
		}
	}
	linesMu.Lock()
	output := strings.Join(lines, "\n")
	linesMu.Unlock()
	return Result{ExitCode: code, Output: output, Err: err}
}

func errorAs(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}
func safeCommand(name string, args []string) string {
	safe := make([]string, len(args))
	redactNext := false
	for i, a := range args {
		lower := strings.ToLower(a)
		if redactNext {
			safe[i] = "<redacted>"
			redactNext = false
		} else if strings.HasPrefix(lower, "--defaults-extra-file=") {
			safe[i] = "--defaults-extra-file=<redacted>"
		} else if lower == "--password-file" || lower == "--password-command" || lower == "--key-file" {
			safe[i] = a
			redactNext = true
		} else if strings.Contains(lower, "password=") || strings.Contains(lower, "token=") || strings.Contains(lower, "secret=") || strings.Contains(lower, "authorization=") {
			safe[i] = Redact(a)
		} else {
			safe[i] = a
		}
	}
	return name + " " + strings.Join(safe, " ")
}

var sensitive = regexp.MustCompile(`(?i)(password|passwd|token|secret|authorization|access[_-]?key)([=: ]+)([^\s,;]+)`)

func Redact(s string) string { return sensitive.ReplaceAllString(s, "$1$2<redacted>") }
