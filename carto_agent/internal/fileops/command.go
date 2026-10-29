package fileops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type boundedOutput struct {
	mu         sync.Mutex
	head, tail []byte
	total      int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	b.total += n
	if len(b.head) < maxTextBytes/2 {
		take := min(len(p), maxTextBytes/2-len(b.head))
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	if len(p) > 0 {
		const tailLimit = maxTextBytes / 2
		if len(p) >= tailLimit {
			b.tail = append(b.tail[:0], p[len(p)-tailLimit:]...)
		} else {
			if discard := len(b.tail) + len(p) - tailLimit; discard > 0 {
				copy(b.tail, b.tail[discard:])
				b.tail = b.tail[:len(b.tail)-discard]
			}
			b.tail = append(b.tail, p...)
		}
	}
	return n, nil
}
func (b *boundedOutput) result() (string, bool, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	truncated := b.total > maxTextBytes
	text := string(b.head)
	if truncated {
		text += "\n[中间输出已截断]\n"
	}
	text += string(b.tail)
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	return text, truncated, b.total
}

func commandEnvironment(output string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "API_KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") {
			continue
		}
		if upper == "AUTOCARTO_OUTPUT_DIR" {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "AUTOCARTO_OUTPUT_DIR="+output)
}

func (w workspace) run(ctx context.Context, args map[string]any) (any, error) {
	command := stringArg(args, "command")
	if command == "" {
		return nil, fmt.Errorf("command 不能为空")
	}
	seconds, err := integer(args, "timeout_seconds", 120, 1, 600)
	if err != nil {
		return nil, err
	}
	workdir := stringArg(args, "workdir")
	if workdir == "" {
		workdir = w.output
	}
	workdir, err = w.resolve(workdir, true)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(workdir, 0755); err != nil {
		return nil, err
	}
	workdir, err = w.resolve(workdir, true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	cmd := localCommand(ctx, command)
	cmd.Dir = workdir
	cmd.Env = commandEnvironment(w.output)
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	started := time.Now()
	err = cmd.Run()
	text, truncated, total := output.result()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	} else if err != nil {
		code = -1
	}
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	cancelled := errors.Is(ctx.Err(), context.Canceled)
	result := map[string]any{"success": err == nil && !timedOut && !cancelled, "workdir": workdir, "output": text, "exit_code": code, "timed_out": timedOut, "cancelled": cancelled, "truncated": truncated, "output_bytes": total, "duration_ms": time.Since(started).Milliseconds(), "execution_environment": "local", "sandboxed": false}
	if err != nil {
		result["error"] = err.Error()
	}
	if timedOut {
		result["error"] = fmt.Sprintf("命令超过 %d 秒，已终止", seconds)
	}
	if cancelled {
		result["error"] = "命令已取消"
	}
	return result, nil
}
