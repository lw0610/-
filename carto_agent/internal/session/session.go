package session

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

// Session 会话结构，维护单个用户的对话历史
type Session struct {
	ID           string
	Messages     []*schema.Message
	PlanContext  *PlanContext
	CreatedAt    time.Time
	LastActiveAt time.Time
	MaxMessages  int // 最大消息数量，超过后截断
	mu           sync.RWMutex
}

// NewSession 创建新会话
func NewSession(id string, maxMessages int) *Session {
	if maxMessages <= 0 {
		maxMessages = 50 // 默认保留50条消息
	}
	return &Session{
		ID:           id,
		Messages:     make([]*schema.Message, 0),
		PlanContext:  nil,
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
		MaxMessages:  maxMessages,
	}
}

// AddMessage 添加消息到历史
func (s *Session) AddMessage(msg *schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Messages = append(s.Messages, msg)
	s.LastActiveAt = time.Now()

	// 如果超过最大消息数，保留系统消息和最近的消息
	s.truncateIfNeeded()
}

// AddMessages 批量添加消息
func (s *Session) AddMessages(msgs []*schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Messages = append(s.Messages, msgs...)
	s.LastActiveAt = time.Now()
	s.truncateIfNeeded()
}

// GetMessages 获取所有消息（返回副本）
func (s *Session) GetMessages() []*schema.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*schema.Message, len(s.Messages))
	copy(result, s.Messages)
	return result
}

// GetMessagesWithSystem 获取消息，确保包含系统提示词
func (s *Session) GetMessagesWithSystem(systemPrompt string) []*schema.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*schema.Message, 0, len(s.Messages)+1)

	// 添加系统提示词
	result = append(result, &schema.Message{
		Role:    schema.System,
		Content: systemPrompt,
	})

	// 添加历史消息（跳过已有的系统消息）
	for _, msg := range s.Messages {
		if msg.Role != schema.System {
			result = append(result, msg)
		}
	}

	return result
}

// Clear 清空会话历史
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Messages = make([]*schema.Message, 0)
	s.PlanContext = nil
	s.LastActiveAt = time.Now()
}

// SetPlanContext 设置规划上下文。
func (s *Session) SetPlanContext(plan *PlanContext) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.PlanContext = clonePlanContext(plan)
	s.LastActiveAt = time.Now()
}

// GetPlanContext 获取规划上下文副本。
func (s *Session) GetPlanContext() *PlanContext {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return clonePlanContext(s.PlanContext)
}

// ClearPlanContext 清除规划上下文。
func (s *Session) ClearPlanContext() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.PlanContext = nil
	s.LastActiveAt = time.Now()
}

// Restore 用持久化状态覆盖当前会话内容。
func (s *Session) Restore(messages []*schema.Message, plan *PlanContext, createdAt, lastActiveAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Messages = cloneMessages(messages)
	s.PlanContext = clonePlanContext(plan)
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if lastActiveAt.IsZero() {
		lastActiveAt = createdAt
	}
	s.CreatedAt = createdAt
	s.LastActiveAt = lastActiveAt
	s.truncateIfNeeded()
}

// truncateIfNeeded 截断消息历史（内部方法，需要持有锁）
func (s *Session) truncateIfNeeded() {
	if len(s.Messages) <= s.MaxMessages {
		return
	}

	// 保留系统消息和最近的消息
	var systemMsgs []*schema.Message
	var otherMsgs []*schema.Message

	for _, msg := range s.Messages {
		if msg.Role == schema.System {
			systemMsgs = append(systemMsgs, msg)
		} else {
			otherMsgs = append(otherMsgs, msg)
		}
	}

	// 计算需要保留的非系统消息数量
	keepCount := max(s.MaxMessages-len(systemMsgs), 0)

	// 保留最近的消息
	if len(otherMsgs) > keepCount {
		otherMsgs = otherMsgs[len(otherMsgs)-keepCount:]
	}

	// 重建消息列表
	s.Messages = append(systemMsgs, otherMsgs...)
}

// MessageCount 返回消息数量
func (s *Session) MessageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Messages)
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return nil
	}

	raw, err := json.Marshal(messages)
	if err != nil {
		out := make([]*schema.Message, len(messages))
		copy(out, messages)
		return out
	}

	var cloned []*schema.Message
	if err := json.Unmarshal(raw, &cloned); err != nil {
		out := make([]*schema.Message, len(messages))
		copy(out, messages)
		return out
	}
	return cloned
}
