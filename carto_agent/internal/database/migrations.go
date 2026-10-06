package database

import (
	"fmt"
)

// migrate 执行数据库迁移
func (d *DB) migrate() error {
	migrations := []string{
		migrationV1Services,
		migrationV1Users,
		migrationV1AgentConfigs,
		migrationV1Indexes,
	}

	for i, migration := range migrations {
		if _, err := d.db.Exec(migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}

	return nil
}

// 服务注册表
const migrationV1Services = `
CREATE TABLE IF NOT EXISTS services (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service_name TEXT NOT NULL UNIQUE,
    instance_id TEXT NOT NULL,
    port INTEGER NOT NULL,
    pid INTEGER,
    status TEXT DEFAULT 'stopped',
    started_at TIMESTAMP,
    last_heartbeat TIMESTAMP,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

// 用户表
const migrationV1Users = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    is_default BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

// 智能体配置表
const migrationV1AgentConfigs = `
CREATE TABLE IF NOT EXISTS agent_configs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    config_name TEXT NOT NULL,
    is_default BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT FALSE,
    config_data TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE,
    UNIQUE(user_id, config_name)
);
`

// 索引
const migrationV1Indexes = `
CREATE INDEX IF NOT EXISTS idx_services_name ON services(service_name);
CREATE INDEX IF NOT EXISTS idx_services_status ON services(status);
CREATE INDEX IF NOT EXISTS idx_users_active ON users(is_active);
CREATE INDEX IF NOT EXISTS idx_agent_configs_user ON agent_configs(user_id);
CREATE INDEX IF NOT EXISTS idx_agent_configs_active ON agent_configs(is_active);
`
