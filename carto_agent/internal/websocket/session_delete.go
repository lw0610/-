package websocket

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const sessionDeleteWaitTimeout = 10 * time.Second

type sessionTaskWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Register before launching so deletion cannot miss a runner which has not yet
// published its runtime state. Multiple retiring runners can share a session.
func (s *DeepServer) startSessionTask(ctx context.Context, conn *websocket.Conn, sessionID string, cancel context.CancelFunc, run func()) bool {
	sid := strings.TrimSpace(sessionID)
	worker := &sessionTaskWorker{cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	if s.deletedSessions[sid] {
		s.mu.Unlock()
		cancel()
		return false
	}
	if s.sessionTaskWorkers == nil {
		s.sessionTaskWorkers = make(map[string]map[*sessionTaskWorker]struct{})
	}
	if s.sessionTaskWorkers[sid] == nil {
		s.sessionTaskWorkers[sid] = make(map[*sessionTaskWorker]struct{})
	}
	s.sessionTaskWorkers[sid][worker] = struct{}{}
	if conn != nil {
		if s.taskCancels[conn] == nil {
			s.taskCancels[conn] = make(map[string]context.CancelFunc)
		}
		s.taskCancels[conn][sid] = cancel
	}
	s.sessionTaskCancels[sid] = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.sessionTaskWorkers[sid], worker)
			if len(s.sessionTaskWorkers[sid]) == 0 {
				delete(s.sessionTaskWorkers, sid)
			}
			close(worker.done)
			s.mu.Unlock()
		}()
		if ctx.Err() == nil {
			run()
		}
	}()
	return true
}

func (s *DeepServer) sessionWasDeleted(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deletedSessions[strings.TrimSpace(sessionID)]
}

// The marker also rejects delayed client saves. It remains after a successful
// deletion; a new conversation uses a new ID. Failed deletions retain files.
func (s *DeepServer) beginSessionDeletion(sessionID string) ([]*sessionTaskWorker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deletedSessions[sessionID] {
		return nil, fmt.Errorf("会话已删除或正在删除: %s", sessionID)
	}
	if s.deletedSessions == nil {
		s.deletedSessions = make(map[string]bool)
	}
	s.deletedSessions[sessionID] = true
	workers := make([]*sessionTaskWorker, 0, len(s.sessionTaskWorkers[sessionID]))
	for worker := range s.sessionTaskWorkers[sessionID] {
		workers = append(workers, worker)
	}
	return workers, nil
}

func (s *DeepServer) abortSessionDeletion(sessionID string) {
	s.mu.Lock()
	delete(s.deletedSessions, sessionID)
	s.mu.Unlock()
}

func waitSessionWorkers(workers []*sessionTaskWorker, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for _, worker := range workers {
		select {
		case <-worker.done:
		case <-timer.C:
			return fmt.Errorf("会话任务尚未结束，文件已保留，请稍后重试删除")
		}
	}
	return nil
}
