package config

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// User 用户信息
type User struct {
	ID          int64     `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsDefault   bool      `json:"is_default"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UserManager 用户管理器
type UserManager struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewUserManager 创建用户管理器
func NewUserManager(db *sql.DB) *UserManager {
	return &UserManager{db: db}
}

// EnsureDefaultUser 确保默认用户存在
func (um *UserManager) EnsureDefaultUser() error {
	um.mu.Lock()
	defer um.mu.Unlock()

	var count int
	err := um.db.QueryRow("SELECT COUNT(*) FROM users WHERE is_default = TRUE").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		userID := uuid.New().String()
		_, err = um.db.Exec(
			`INSERT INTO users (user_id, name, description, is_default, is_active)
			 VALUES (?, ?, ?, TRUE, TRUE)`,
			userID, "默认用户", "系统默认用户",
		)
		if err != nil {
			return fmt.Errorf("创建默认用户失败: %w", err)
		}
	}
	return nil
}

// ListUsers 列出所有用户
func (um *UserManager) ListUsers() ([]User, error) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	rows, err := um.db.Query(
		`SELECT id, user_id, name, description, is_default, is_active, created_at, updated_at
		 FROM users ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		err := rows.Scan(&u.ID, &u.UserID, &u.Name, &u.Description,
			&u.IsDefault, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetUser 获取用户
func (um *UserManager) GetUser(userID string) (*User, error) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	var u User
	err := um.db.QueryRow(
		`SELECT id, user_id, name, description, is_default, is_active, created_at, updated_at
		 FROM users WHERE user_id = ?`, userID,
	).Scan(&u.ID, &u.UserID, &u.Name, &u.Description,
		&u.IsDefault, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetActiveUser 获取当前激活用户
func (um *UserManager) GetActiveUser() (*User, error) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	var u User
	err := um.db.QueryRow(
		`SELECT id, user_id, name, description, is_default, is_active, created_at, updated_at
		 FROM users WHERE is_active = TRUE`,
	).Scan(&u.ID, &u.UserID, &u.Name, &u.Description,
		&u.IsDefault, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateUser 创建用户
func (um *UserManager) CreateUser(name, description string) (*User, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	userID := uuid.New().String()
	now := time.Now()

	_, err := um.db.Exec(
		`INSERT INTO users (user_id, name, description, is_default, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, FALSE, FALSE, ?, ?)`,
		userID, name, description, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	return &User{
		UserID:      userID,
		Name:        name,
		Description: description,
		IsDefault:   false,
		IsActive:    false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// SwitchUser 切换激活用户
func (um *UserManager) SwitchUser(userID string) error {
	um.mu.Lock()
	defer um.mu.Unlock()

	// 先取消所有用户的激活状态
	_, err := um.db.Exec("UPDATE users SET is_active = FALSE, updated_at = ?", time.Now())
	if err != nil {
		return fmt.Errorf("取消激活状态失败: %w", err)
	}

	// 激活指定用户
	result, err := um.db.Exec(
		"UPDATE users SET is_active = TRUE, updated_at = ? WHERE user_id = ?",
		time.Now(), userID,
	)
	if err != nil {
		return fmt.Errorf("激活用户失败: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("用户不存在: %s", userID)
	}
	return nil
}

// DeleteUser 删除用户
func (um *UserManager) DeleteUser(userID string) error {
	um.mu.Lock()
	defer um.mu.Unlock()

	// 检查是否为默认用户
	var isDefault bool
	err := um.db.QueryRow(
		"SELECT is_default FROM users WHERE user_id = ?", userID,
	).Scan(&isDefault)
	if err == sql.ErrNoRows {
		return fmt.Errorf("用户不存在: %s", userID)
	}
	if err != nil {
		return err
	}
	if isDefault {
		return fmt.Errorf("默认用户不能删除")
	}

	// 删除用户
	_, err = um.db.Exec("DELETE FROM users WHERE user_id = ?", userID)
	return err
}
