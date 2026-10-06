package session

import (
	"sync"
	"time"
)

// Manager 会话管理器，管理多个用户会话
type Manager struct {
	sessions    map[string]*Session
	maxMessages int
	ttl         time.Duration // 会话过期时间
	mu          sync.RWMutex
}

// NewManager 创建会话管理器
func NewManager(maxMessages int, ttl time.Duration) *Manager {
	if maxMessages <= 0 {
		maxMessages = 50
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	m := &Manager{
		sessions:    make(map[string]*Session),
		maxMessages: maxMessages,
		ttl:         ttl,
	}

	// 启动清理协程
	go m.cleanupLoop()

	return m
}

// GetOrCreate 获取或创建会话
func (m *Manager) GetOrCreate(sessionID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[sessionID]; ok {
		session.LastActiveAt = time.Now()
		return session
	}

	session := NewSession(sessionID, m.maxMessages)
	m.sessions[sessionID] = session
	return session
}

// Get 获取会话（不存在返回nil）
func (m *Manager) Get(sessionID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[sessionID]
}

// Delete 删除会话
func (m *Manager) Delete(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// Clear 清空指定会话的历史
func (m *Manager) Clear(sessionID string) {
	m.mu.RLock()
	session := m.sessions[sessionID]
	m.mu.RUnlock()

	if session != nil {
		session.Clear()
	}
}

// cleanupLoop 定期清理过期会话
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanup()
	}
}

// cleanup 清理过期会话
func (m *Manager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, session := range m.sessions {
		if now.Sub(session.LastActiveAt) > m.ttl {
			delete(m.sessions, id)
		}
	}
}

// SessionCount 返回活跃会话数量
func (m *Manager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
