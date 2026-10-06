package discovery

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/autocarto/carto_agent/internal/config"
)

// ServiceRegistry 服务端口注册表。
type ServiceRegistry struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewServiceRegistry 创建服务端口注册表。
func NewServiceRegistry(dbPath string) (*ServiceRegistry, error) {
	resolvedPath, err := resolveServiceRegistryDBPath(dbPath)
	if err != nil {
		return nil, err
	}

	if err := ensureServiceRegistryDir(resolvedPath); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("打开服务注册 SQLite 失败: %w", err)
	}

	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("设置服务注册 SQLite WAL 失败: %w", err)
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS service_ports (
	service_name TEXT PRIMARY KEY,
	host TEXT NOT NULL DEFAULT '127.0.0.1',
	port INTEGER NOT NULL,
	protocol TEXT NOT NULL DEFAULT 'http',
	pid INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'running',
	registered_at TEXT NOT NULL,
	last_seen TEXT NOT NULL
)
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 service_ports 表失败: %w", err)
	}

	return &ServiceRegistry{db: db}, nil
}

// Register 注册或更新服务端口信息。
func (r *ServiceRegistry) Register(entry ServicePortEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.db == nil {
		return fmt.Errorf("服务注册表未初始化")
	}

	normalized := normalizeServicePortEntry(entry)
	_, err := r.db.Exec(`
INSERT OR REPLACE INTO service_ports (
	service_name,
	host,
	port,
	protocol,
	pid,
	status,
	registered_at,
	last_seen
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`,
		normalized.ServiceName,
		normalized.Host,
		normalized.Port,
		normalized.Protocol,
		normalized.PID,
		normalized.Status,
		normalized.RegisteredAt,
		normalized.LastSeen,
	)
	if err != nil {
		return fmt.Errorf("注册服务端口失败: %w", err)
	}
	return nil
}

// Unregister 注销服务端口信息。
func (r *ServiceRegistry) Unregister(serviceName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.db == nil {
		return fmt.Errorf("服务注册表未初始化")
	}

	result, err := r.db.Exec(`DELETE FROM service_ports WHERE service_name = ?`, strings.TrimSpace(serviceName))
	if err != nil {
		return fmt.Errorf("注销服务端口失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("服务 %s 未注册", serviceName)
	}
	return nil
}

// GetPort 获取单个服务的端口信息。
func (r *ServiceRegistry) GetPort(serviceName string) (ServicePortEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.db == nil {
		return ServicePortEntry{}, fmt.Errorf("服务注册表未初始化")
	}

	row := r.db.QueryRow(`
SELECT service_name, host, port, protocol, pid, status, registered_at, last_seen
FROM service_ports
WHERE service_name = ?
LIMIT 1
`, strings.TrimSpace(serviceName))

	entry, err := scanServicePortEntry(row)
	if err != nil {
		return ServicePortEntry{}, err
	}
	return entry, nil
}

// GetAllPorts 获取所有服务端口信息。
func (r *ServiceRegistry) GetAllPorts() ([]ServicePortEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.db == nil {
		return nil, fmt.Errorf("服务注册表未初始化")
	}

	rows, err := r.db.Query(`
SELECT service_name, host, port, protocol, pid, status, registered_at, last_seen
FROM service_ports
ORDER BY service_name
`)
	if err != nil {
		return nil, fmt.Errorf("查询所有服务端口失败: %w", err)
	}
	defer rows.Close()

	entries := make([]ServicePortEntry, 0)
	for rows.Next() {
		entry, err := scanServicePortEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描服务端口失败: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历服务端口失败: %w", err)
	}
	return entries, nil
}

// Heartbeat 更新服务最近心跳时间。
func (r *ServiceRegistry) Heartbeat(serviceName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.db == nil {
		return fmt.Errorf("服务注册表未初始化")
	}

	result, err := r.db.Exec(`
UPDATE service_ports
SET last_seen = ?
WHERE service_name = ?
`, time.Now().UTC().Format(time.RFC3339Nano), strings.TrimSpace(serviceName))
	if err != nil {
		return fmt.Errorf("更新服务心跳失败: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取服务心跳更新结果失败: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CleanExpired 清理超过最大存活时间的服务记录。
func (r *ServiceRegistry) CleanExpired(maxAge time.Duration) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.db == nil {
		return 0, fmt.Errorf("服务注册表未初始化")
	}

	if maxAge < 0 {
		maxAge = 0
	}

	rows, err := r.db.Query(`SELECT service_name, last_seen FROM service_ports`)
	if err != nil {
		return 0, fmt.Errorf("查询待清理服务记录失败: %w", err)
	}
	defer rows.Close()

	cutoff := time.Now().UTC().Add(-maxAge)
	toDelete := make([]string, 0)
	for rows.Next() {
		var serviceName string
		var lastSeen string
		if err := rows.Scan(&serviceName, &lastSeen); err != nil {
			return 0, fmt.Errorf("扫描待清理服务记录失败: %w", err)
		}

		parsed := parseTime(lastSeen)
		if parsed.IsZero() || parsed.Before(cutoff) {
			toDelete = append(toDelete, serviceName)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("遍历待清理服务记录失败: %w", err)
	}

	removed := 0
	for _, serviceName := range toDelete {
		result, err := r.db.Exec(`DELETE FROM service_ports WHERE service_name = ?`, serviceName)
		if err != nil {
			return removed, fmt.Errorf("删除过期服务记录失败: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return removed, fmt.Errorf("读取过期服务删除结果失败: %w", err)
		}
		removed += int(affected)
	}

	return removed, nil
}

// Close 关闭服务注册表。
func (r *ServiceRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.db == nil {
		return nil
	}

	db := r.db
	r.db = nil
	if err := db.Close(); err != nil {
		return fmt.Errorf("关闭服务注册表失败: %w", err)
	}
	return nil
}

func resolveServiceRegistryDBPath(dbPath string) (string, error) {
	trimmed := strings.TrimSpace(dbPath)
	if trimmed != "" {
		return trimmed, nil
	}

	dataDir, err := config.ResolvePluginDataDir()
	if err != nil {
		return "", fmt.Errorf("解析服务注册目录失败: %w", err)
	}
	return filepath.Join(dataDir, "instances.db"), nil
}

func ensureServiceRegistryDir(dbPath string) error {
	trimmed := strings.TrimSpace(dbPath)
	if trimmed == "" || trimmed == ":memory:" || strings.HasPrefix(trimmed, "file:") {
		return nil
	}

	dir := filepath.Dir(trimmed)
	if dir == "." || dir == "" {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建服务注册目录失败: %w", err)
	}
	return nil
}

func normalizeServicePortEntry(entry ServicePortEntry) ServicePortEntry {
	now := time.Now().UTC()

	serviceName := strings.TrimSpace(entry.ServiceName)
	host := strings.TrimSpace(entry.Host)
	if host == "" {
		host = "127.0.0.1"
	}

	protocol := strings.TrimSpace(entry.Protocol)
	if protocol == "" {
		protocol = "http"
	}

	status := strings.TrimSpace(entry.Status)
	if status == "" {
		status = "running"
	}

	registeredAt := strings.TrimSpace(entry.RegisteredAt)
	if registeredAt == "" {
		registeredAt = now.Format(time.RFC3339Nano)
	}

	lastSeen := strings.TrimSpace(entry.LastSeen)
	if lastSeen == "" {
		lastSeen = now.Format(time.RFC3339Nano)
	}

	return ServicePortEntry{
		ServiceName:  serviceName,
		Host:         host,
		Port:         entry.Port,
		Protocol:     protocol,
		PID:          entry.PID,
		Status:       status,
		RegisteredAt: registeredAt,
		LastSeen:     lastSeen,
	}
}

func scanServicePortEntry(scanner rowScanner) (ServicePortEntry, error) {
	entry := ServicePortEntry{}
	if err := scanner.Scan(
		&entry.ServiceName,
		&entry.Host,
		&entry.Port,
		&entry.Protocol,
		&entry.PID,
		&entry.Status,
		&entry.RegisteredAt,
		&entry.LastSeen,
	); err != nil {
		return ServicePortEntry{}, err
	}
	return entry, nil
}
