package knowledge

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ProgressExtraKey marks application status, never model reasoning or a final
// assistant answer. Message consumers must route it separately from both.
const ProgressExtraKey = "autocarto_runtime_progress"

const requestHeartbeatInterval = 20 * time.Second

type requestProgressKey struct{}

func withRequestProgress(ctx context.Context, progress func(Progress)) context.Context {
	return context.WithValue(ctx, requestProgressKey{}, progress)
}

func requestProgressFromContext(ctx context.Context) func(Progress) {
	progress, _ := ctx.Value(requestProgressKey{}).(func(Progress))
	return progress
}

func progressForAttempt(a Attempt, elapsed int64) Progress {
	return Progress{Stage: a.Stage, ID: a.ID, Attempt: a.Number, Status: a.Status, StartedAt: a.StartedAt, ElapsedMillis: elapsed}
}

// Generate stays on the caller goroutine and receives its cancellable context.
// This goroutine only emits immutable progress, never touches Result, and is
// joined before any subsequent progress or return. Callbacks must not block.
func startRequestHeartbeat(ctx context.Context, progress func(Progress), attempt Attempt, started time.Time, interval time.Duration) func() {
	if progress == nil {
		return func() {}
	}
	progress(progressForAttempt(attempt, 0))
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				progress(progressForAttempt(attempt, time.Since(started).Milliseconds()))
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }); <-done }
}

func (p Progress) Text() string {
	stage := p.Stage
	switch stage {
	case "extract":
		stage = "分段抽取"
	case "semantic_review":
		stage = "统一语义核对"
	}
	if p.ID == "" {
		return fmt.Sprintf("知识抽取：%s（%d/%d）", stage, p.Current, p.Total)
	}
	seconds := max(int64(0), p.ElapsedMillis/1000)
	state := fmt.Sprintf("等待模型，已等待 %d 秒", seconds)
	switch p.Status {
	case "response_received":
		state = fmt.Sprintf("模型已返回，耗时 %d 秒；正在校验", seconds)
	case "failed":
		state = fmt.Sprintf("请求失败，耗时 %d 秒", seconds)
	case "cancelled":
		state = fmt.Sprintf("请求已取消，耗时 %d 秒", seconds)
	}
	return fmt.Sprintf("知识抽取：%s · %s · 第 %d 次请求 · %s", stage, p.ID, p.Attempt, state)
}

func (p Progress) EventData() map[string]any {
	data := map[string]any{"stage": p.Stage, "status": p.Status, "elapsed_ms": p.ElapsedMillis}
	if p.ID != "" {
		data["request_id"], data["attempt"], data["started_at"] = p.ID, p.Attempt, p.StartedAt
		data["request_key"] = fmt.Sprintf("%s/%s/%d/%s", p.Stage, p.ID, p.Attempt, p.StartedAt)
	}
	return data
}
