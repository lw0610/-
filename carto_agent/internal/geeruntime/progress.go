package geeruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Progress is application data from the workflow/download helpers, never model reasoning.
type Progress struct {
	RequestKey     string   `json:"request_key,omitempty"`
	Type           string   `json:"type"`
	DownloadID     int      `json:"download_id"`
	Filename       string   `json:"filename"`
	Phase          string   `json:"phase"`
	Percent        *float64 `json:"percent"`
	CompletedBytes int64    `json:"completed_bytes,omitempty"`
	TotalBytes     int64    `json:"total_bytes,omitempty"`
	StageID        int      `json:"stage_id,omitempty"`
	Stage          string   `json:"stage,omitempty"`
	ElapsedSeconds int64    `json:"elapsed_seconds,omitempty"`
	Reused         bool     `json:"reused,omitempty"`
}

type progressCallbackKey struct{}

func WithProgressCallback(ctx context.Context, callback func(Progress)) context.Context {
	if callback == nil {
		return ctx
	}
	return context.WithValue(ctx, progressCallbackKey{}, callback)
}

func (p Progress) Text() string {
	if p.Type == "workflow_progress" {
		switch p.Phase {
		case "running":
			return fmt.Sprintf("GEE 正在处理：%s（已用时 %d 秒）", p.Stage, p.ElapsedSeconds)
		case "completed":
			return "GEE 阶段已完成：" + p.Stage
		case "failed":
			return "GEE 阶段失败：" + p.Stage
		}
		return ""
	}
	name := filepath.Base(p.Filename)
	switch p.Phase {
	case "preparing":
		return "GEE 正在计算下载范围和分块：" + name
	case "downloading":
		if p.Percent != nil {
			return fmt.Sprintf("GEE 正在下载 %s：%.0f%%", name, *p.Percent)
		}
		return "GEE 正在下载：" + name
	case "validating":
		return "GEE 正在检查影像完整性：" + name
	case "completed":
		if p.Reused {
			return "GEE 已复用此前完成的影像（未重复下载）：" + name
		}
		return "GEE 文件已下载并通过完整性检查：" + name
	case "failed":
		return "GEE 文件下载失败：" + name
	}
	return ""
}

// Poll only the bounded tail of the journal. No user stdout, signed URL or
// credential is forwarded; the helper's known fields form the entire UI event.
func latestProgress(path string) (Progress, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Progress{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Progress{}, false
	}
	const tailSize = 64 * 1024
	start := max(int64(0), info.Size()-tailSize)
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return Progress{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, tailSize))
	if err != nil {
		return Progress{}, false
	}
	if start > 0 {
		if split := bytes.IndexByte(data, '\n'); split >= 0 {
			data = data[split+1:]
		} else {
			return Progress{}, false
		}
	}
	lines := bytes.Split(data, []byte{'\n'})
	// A line is published only once its trailing newline has been written.
	for i := len(lines) - 2; i >= 0; i-- {
		if len(lines[i]) > 4096 {
			continue
		}
		var p Progress
		if json.Unmarshal(lines[i], &p) != nil {
			continue
		}
		if p.Type == "workflow_progress" {
			if p.StageID < 1 || p.ElapsedSeconds < 0 || len(p.Stage) > 512 || strings.TrimSpace(p.Stage) == "" || p.Text() == "" {
				continue
			}
			p.Stage = strings.Join(strings.Fields(p.Stage), " ")
			p.RequestKey = fmt.Sprintf("gee:%s:stage:%d", filepath.Base(filepath.Dir(path)), p.StageID)
			return p, true
		}
		if p.Type != "download_progress" || p.DownloadID < 0 {
			continue
		}
		if p.Percent != nil && (*p.Percent < 0 || *p.Percent > 100) {
			continue
		}
		if strings.TrimSpace(p.Filename) == "" || len(p.Filename) > 512 || p.Text() == "" {
			continue
		}
		p.Filename = filepath.Base(p.Filename)
		// The process boundary supplies identity; separate scripts both start at 1.
		p.RequestKey = fmt.Sprintf("gee:%s:%d", filepath.Base(filepath.Dir(path)), p.DownloadID)
		return p, true
	}
	return Progress{}, false
}

func watchProgress(ctx context.Context, path string, callback func(Progress)) func() {
	if callback == nil {
		return func() {}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		last := ""
		poll := func() {
			if p, ok := latestProgress(path); ok {
				encoded, _ := json.Marshal(p)
				if string(encoded) != last {
					last = string(encoded)
					callback(p)
				}
			}
		}
		for {
			select {
			case <-ticker.C:
				poll()
			case <-stop:
				poll()
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		close(stop)
		// Display callbacks must never prevent Python cancellation or tool return.
		timer := time.NewTimer(1500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-done:
		case <-ctx.Done():
		case <-timer.C:
		}
	}
}
