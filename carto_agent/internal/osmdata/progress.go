package osmdata

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/google/uuid"
)

// Progress describes an individual acquisition, not model reasoning. Parallel
// tools have distinct keys even when their tool names and output names match.
type Progress struct {
	RequestKey, Tool, Filename, Phase, Endpoint, Message         string
	Attempt, MaxAttempts, ExtentIndex, ExtentCount, FeatureCount int
	CompletedBytes, TotalBytes                                   int64
	ElapsedSeconds                                               float64
	// BytesPerSecond measures bytes delivered to the application (after any
	// automatic HTTP decompression), not the network link's transfer rate.
	BytesPerSecond float64
	// Percent is absent without a reliable response size. Receipt of the whole
	// response is a processing phase; 100 is reserved for successfully saved output.
	Percent       *float64
	ArtifactPaths []string
}

type progressCallbackKey struct{}
type progressStateKey struct{}
type progressMetadataKey struct{}
type progressDisabledKey struct{}

func WithProgressCallback(ctx context.Context, callback func(Progress)) context.Context {
	return context.WithValue(ctx, progressCallbackKey{}, callback)
}

// ProgressCallback lets a trusted orchestration stage keep one download card
// open through local postprocessing without exposing temporary artifacts.
func ProgressCallback(ctx context.Context) func(Progress) {
	callback, _ := ctx.Value(progressCallbackKey{}).(func(Progress))
	return callback
}

func withoutProgress(ctx context.Context) context.Context {
	return context.WithValue(ctx, progressDisabledKey{}, true)
}

func withProgressMetadata(ctx context.Context, p Progress) context.Context {
	old, _ := ctx.Value(progressMetadataKey{}).(Progress)
	if p.RequestKey == "" {
		p.RequestKey = old.RequestKey
	}
	if p.Tool == "" {
		p.Tool = old.Tool
	}
	if p.Filename == "" {
		p.Filename = old.Filename
	}
	if p.Endpoint == "" {
		p.Endpoint = old.Endpoint
	}
	if p.Attempt == 0 {
		p.Attempt = old.Attempt
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = old.MaxAttempts
	}
	if p.ExtentIndex == 0 {
		p.ExtentIndex = old.ExtentIndex
	}
	if p.ExtentCount == 0 {
		p.ExtentCount = old.ExtentCount
	}
	return context.WithValue(ctx, progressMetadataKey{}, p)
}

type progressState struct {
	mu                           sync.Mutex
	started, emittedAt, loggedAt time.Time
	last                         Progress
	terminal                     bool
	rate                         progressRate
}

// progressRate samples actual byte deltas over recent intervals. Keeping a
// single checkpoint bounds memory even when a server returns tiny chunks.
type progressRate struct {
	endpoint                   string
	attempt, extent            int
	checkpointAt, lastGrowth   time.Time
	checkpointBytes, lastBytes int64
	bytesPerSecond             float64
}

func (r *progressRate) sample(p Progress, now time.Time) float64 {
	if r.checkpointAt.IsZero() || r.endpoint != p.Endpoint || r.attempt != p.Attempt || r.extent != p.ExtentIndex || p.CompletedBytes < r.lastBytes {
		*r = progressRate{endpoint: p.Endpoint, attempt: p.Attempt, extent: p.ExtentIndex,
			checkpointAt: now, lastGrowth: now, checkpointBytes: p.CompletedBytes, lastBytes: p.CompletedBytes}
		return 0
	}
	if p.CompletedBytes > r.lastBytes {
		r.lastGrowth = now
	}
	r.lastBytes = p.CompletedBytes
	if elapsed := now.Sub(r.checkpointAt); elapsed >= time.Second {
		r.bytesPerSecond = float64(p.CompletedBytes-r.checkpointBytes) / elapsed.Seconds()
		r.checkpointAt, r.checkpointBytes = now, p.CompletedBytes
	}
	// A heartbeat with no newly received bytes must not keep displaying the
	// speed from a previous burst indefinitely.
	if now.Sub(r.lastGrowth) >= 5*time.Second {
		r.bytesPerSecond = 0
	}
	return r.bytesPerSecond
}

func responseReceived(p Progress) bool {
	return p.TotalBytes > 0 && p.CompletedBytes >= p.TotalBytes
}

// updateMetrics runs under progressState.mu for byte events and heartbeats.
func (s *progressState) updateMetrics(p *Progress, now time.Time) {
	p.BytesPerSecond, p.Percent = 0, nil
	if p.Phase == "downloading" {
		p.BytesPerSecond = s.rate.sample(*p, now)
		if p.TotalBytes > 0 && p.CompletedBytes >= 0 && !responseReceived(*p) {
			percent := float64(p.CompletedBytes) / float64(p.TotalBytes) * 100
			p.Percent = &percent
		}
		return
	}
	s.rate = progressRate{}
	if p.Phase == "completed" && len(p.ArtifactPaths) > 0 {
		percent := float64(100)
		p.Percent = &percent
	}
}

func terminalProgress(phase string) bool {
	return phase == "completed" || phase == "partial" || phase == "failed" || phase == "cancelled"
}

// reportProgress coalesces byte updates, but publishes state transitions
// immediately. Serial delivery prevents a late heartbeat overwriting completion.
func reportProgress(ctx context.Context, p Progress) {
	if disabled, _ := ctx.Value(progressDisabledKey{}).(bool); disabled {
		return
	}
	meta, _ := withProgressMetadata(ctx, p).Value(progressMetadataKey{}).(Progress)
	p.RequestKey, p.Tool, p.Filename = meta.RequestKey, meta.Tool, meta.Filename
	p.Endpoint, p.Attempt, p.MaxAttempts = meta.Endpoint, meta.Attempt, meta.MaxAttempts
	p.ExtentIndex, p.ExtentCount = meta.ExtentIndex, meta.ExtentCount
	if address, err := url.Parse(p.Endpoint); err == nil && address.Host != "" {
		p.Endpoint = address.Host
	}
	p.Message = strings.Join(strings.Fields(p.Message), " ")
	if runes := []rune(p.Message); len(runes) > 500 {
		p.Message = string(runes[:500]) + "…"
	}
	callback, _ := ctx.Value(progressCallbackKey{}).(func(Progress))
	state, _ := ctx.Value(progressStateKey{}).(*progressState)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.terminal {
		return
	}
	if ctx.Err() != nil && !terminalProgress(p.Phase) {
		return
	}
	now := time.Now()
	p.ElapsedSeconds = now.Sub(state.started).Seconds()
	state.updateMetrics(&p, now)
	changed := p.Phase != state.last.Phase || p.Endpoint != state.last.Endpoint || p.Attempt != state.last.Attempt || p.ExtentIndex != state.last.ExtentIndex || responseReceived(p) != responseReceived(state.last)
	state.last = p
	state.terminal = terminalProgress(p.Phase)
	if changed || state.terminal || now.Sub(state.loggedAt) >= 30*time.Second {
		logger.Info("OSM 进度: request=%s tool=%s file=%s phase=%s endpoint=%s attempt=%d/%d extent=%d/%d bytes=%d elapsed=%.1fs message=%s", p.RequestKey, p.Tool, p.Filename, p.Phase, p.Endpoint, p.Attempt, p.MaxAttempts, p.ExtentIndex, p.ExtentCount, p.CompletedBytes, p.ElapsedSeconds, p.Message)
		state.loggedAt = now
	}
	if callback != nil && (changed || state.terminal || now.Sub(state.emittedAt) >= time.Second) {
		callback(p)
		state.emittedAt = now
	}
}

func beginProgress(ctx context.Context, tool, filename string) (context.Context, func(*Result, error)) {
	if strings.TrimSpace(filename) == "" {
		filename = "OSM 数据"
	}
	state := &progressState{started: time.Now()}
	ctx = context.WithValue(ctx, progressStateKey{}, state)
	ctx = withProgressMetadata(ctx, Progress{RequestKey: "osm:" + uuid.NewString(), Tool: tool, Filename: filepath.Base(filename)})
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				state.mu.Lock()
				// Read and publish under one lock so a heartbeat cannot race a
				// new attempt or put old byte counts back on screen.
				if !state.terminal && state.last.Phase != "" && ctx.Err() == nil {
					p := state.last
					now := time.Now()
					p.ElapsedSeconds = now.Sub(state.started).Seconds()
					state.updateMetrics(&p, now)
					if callback, _ := ctx.Value(progressCallbackKey{}).(func(Progress)); callback != nil && now.Sub(state.emittedAt) >= time.Second {
						callback(p)
						state.emittedAt = now
					}
					state.last = p
				}
				state.mu.Unlock()
			}
		}
	}()
	reportProgress(ctx, Progress{Phase: "preparing"})
	return ctx, func(result *Result, err error) {
		close(stop)
		p := Progress{Phase: "completed"}
		if result != nil {
			p.FeatureCount, p.ArtifactPaths = result.FeatureCount, append([]string{}, result.ArtifactPaths...)
			p.Message = result.Info
			if result.Partial {
				p.Phase = "partial"
			}
		}
		if err != nil {
			p.Phase, p.Message = "failed", err.Error()
			if errors.Is(err, context.Canceled) {
				p.Phase = "cancelled"
			}
		}
		reportProgress(ctx, p)
	}
}

func (p Progress) Text() string {
	name := filepath.Base(p.Filename)
	if name == "." || name == "" {
		name = "OSM 数据"
	}
	where := ""
	if p.Endpoint != "" {
		where = "（" + p.Endpoint + "）"
	}
	suffix := fmt.Sprintf("，已用时 %.0f 秒", p.ElapsedSeconds)
	switch p.Phase {
	case "preparing":
		return "正在准备下载：" + name
	case "waiting":
		if p.Attempt < 1 || p.MaxAttempts < 1 {
			return "正在请求 " + name + where + suffix
		}
		return fmt.Sprintf("正在请求 %s%s，第 %d/%d 个节点%s", name, where, p.Attempt, p.MaxAttempts, suffix)
	case "downloading":
		amount := fmt.Sprintf("已接收 %.2f MB", float64(p.CompletedBytes)/1e6)
		if p.TotalBytes > 0 {
			amount += fmt.Sprintf(" / %.2f MB", float64(p.TotalBytes)/1e6)
			if responseReceived(p) {
				amount += "，接收完毕，正在处理"
			} else if p.Percent != nil {
				// Do not round an unfinished 99.99% response up to 100%.
				amount += fmt.Sprintf("（%.1f%%）", math.Floor(*p.Percent*10)/10)
			}
		} else {
			amount += "（总大小未知，无法计算百分比）"
		}
		amount += fmt.Sprintf("，接收速度 %.1f KB/s", p.BytesPerSecond/1e3)
		if p.ExtentCount > 1 {
			amount = fmt.Sprintf("第 %d/%d 个范围，", p.ExtentIndex, p.ExtentCount) + amount
		}
		return "正在下载 " + name + where + "：" + amount + suffix
	case "retrying":
		return "正在切换下载节点：" + name + "；" + p.Message + suffix
	case "saving":
		return fmt.Sprintf("正在保存 %s：%d 个要素", name, p.FeatureCount)
	case "completed":
		if len(p.ArtifactPaths) == 0 {
			return "查询完成：" + name + "，没有符合条件的要素"
		}
		return fmt.Sprintf("已下载 %s：100%%，%d 个要素，文件已保存", name, p.FeatureCount)
	case "partial":
		return fmt.Sprintf("%s 部分完成：已保存 %d 个要素；部分范围未完成，详情见工具结果", name, p.FeatureCount)
	case "failed":
		return "下载未完成：" + name + "；" + p.Message
	case "cancelled":
		return "下载已取消：" + name
	}
	return ""
}
