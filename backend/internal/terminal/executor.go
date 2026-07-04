package terminal

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Executor is the single, generic execution layer — the "Terminal MCP".
// The agent runs ANY CLI tool through this; there are no per-tool wrappers and
// no command allowlist (authorized sandbox by design).
type Executor struct {
	shell    []string // e.g. ["/bin/bash", "-lc"]
	workdir  string
	auditLog string
	mu       sync.Mutex
}

func New(shell []string, workdir, auditLog string) *Executor {
	return &Executor{shell: shell, workdir: workdir, auditLog: auditLog}
}

// Result is the outcome of a command.
type Result struct {
	ExitCode int
	Output   string // combined stdout+stderr
	Err      error
}

// Run executes a command, streaming output line-by-line via onChunk(stream, line)
// and returning the combined output. timeout==0 uses defaultTimeout.
func (e *Executor) Run(ctx context.Context, command string, timeout time.Duration, onChunk func(stream, data string)) Result {
	e.audit(command)

	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append(append([]string{}, e.shell[1:]...), command)
	cmd := exec.CommandContext(ctx, e.shell[0], args...)
	cmd.Dir = e.workdir

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	var buf strings.Builder
	var bufMu sync.Mutex
	collect := func(r io.Reader, stream string) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			bufMu.Lock()
			buf.WriteString(line)
			buf.WriteByte('\n')
			bufMu.Unlock()
			if onChunk != nil {
				onChunk(stream, line)
			}
		}
	}

	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1, Output: err.Error(), Err: err}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); collect(stdout, "stdout") }()
	go func() { defer wg.Done(); collect(stderr, "stderr") }()
	wg.Wait()

	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
		if ctx.Err() == context.DeadlineExceeded {
			bufMu.Lock()
			buf.WriteString(fmt.Sprintf("\n[command timed out after %s]\n", timeout))
			bufMu.Unlock()
		}
	}
	return Result{ExitCode: exitCode, Output: buf.String(), Err: err}
}

func (e *Executor) audit(command string) {
	if e.auditLog == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	f, err := os.OpenFile(e.auditLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\t%s\n", time.Now().Format(time.RFC3339), command)
}
