package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// DB 数据库管理器
type DB struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// Config 数据库配置
type Config struct {
	Path string // 数据库文件路径
}

// New 创建数据库管理器
func New(cfg *Config) (*DB, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("database path is required")
	}

	// 确保目录存在
	dir := filepath.Dir(cfg.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	// 打开数据库
	db, err := sql.Open("sqlite3", cfg.Path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	// 设置连接池
	db.SetMaxOpenConns(1) // SQLite 单写入
	db.SetMaxIdleConns(1)

	manager := &DB{
		db:   db,
		path: cfg.Path,
	}

	// 运行迁移
	if err := manager.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return manager, nil
}

// Close 关闭数据库
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Close()
}

// DB 获取底层数据库连接
func (d *DB) DB() *sql.DB {
	return d.db
}

// Path 获取数据库路径
func (d *DB) Path() string {
	return d.path
}
