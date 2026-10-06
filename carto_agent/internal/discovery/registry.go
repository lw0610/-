package discovery

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/logger"
)

const (
	registryEventChBuffer = 64
	defaultHeartbeatTTL   = 45 * time.Second
	defaultCleanupTick    = 5 * time.Second
)

// InstanceRegistry 实例注册表（内存缓存 + SQLite 持久化）
type InstanceRegistry struct {
	mu        sync.RWMutex
	instances map[string]*QGISInstance
	eventCh   chan InstanceEvent
	stopCh    chan struct{}
	cleanupOn bool
	db        *sql.DB
	dbPath    string
}

// NewInstanceRegistry 创建实例注册表
func NewInstanceRegistry() *InstanceRegistry {
	r := &InstanceRegistry{
		instances: make(map[string]*QGISInstance),
		eventCh:   make(chan InstanceEvent, registryEventChBuffer),
		stopCh:    make(chan struct{}),
	}
	r.initSQLite()
	r.loadPersistedInstances()
	return r
}

// Register 注册一个 QGIS 实例（Python/插件反向连接调用）
func (r *InstanceRegistry) Register(req RegisterRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	existing, exists := r.instances[req.InstanceID]

	source := req.Source
	if source == "" {
		source = SourceRegister
	}

	services := buildInstanceServices(source, "running", now)

	inst := &QGISInstance{
		ID:                 req.InstanceID,
		MachineFingerprint: req.MachineFingerprint,
		StartTime:          req.StartTime,
		PID:                req.PID,
		QGISVersion:        req.QGISVersion,
		ProjectPath:        req.ProjectPath,
		Status:             "running",
		Services:           services,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastHeart:          now,
		Source:             source,
	}

	if exists {
		inst.CreatedAt = existing.CreatedAt
		if inst.MachineFingerprint == "" {
			inst.MachineFingerprint = existing.MachineFingerprint
		}
		if inst.StartTime == "" {
			inst.StartTime = existing.StartTime
		}
	}

	r.instances[req.InstanceID] = inst

	eventType := "added"
	if exists {
		eventType = "changed"
	}
	r.emitEvent(InstanceEvent{Type: eventType, Instance: inst})
	r.persistInstance(inst)

	logger.Info("InstanceRegistry: 实例已注册 %s (PID=%d, source=%s)", req.InstanceID, req.PID, source)
	return nil
}

// Unregister 注销实例
func (r *InstanceRegistry) Unregister(instanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.instances[instanceID]
	if !ok {
		return fmt.Errorf("实例不存在: %s", instanceID)
	}

	delete(r.instances, instanceID)
	inst.Status = "stopped"
	inst.UpdatedAt = time.Now()
	r.emitEvent(InstanceEvent{Type: "removed", Instance: inst})
	r.persistInstance(inst)

	logger.Info("InstanceRegistry: 实例已注销 %s", instanceID)
	return nil
}

// Heartbeat 更新实例心跳
func (r *InstanceRegistry) Heartbeat(instanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.instances[instanceID]
	if !ok {
		return fmt.Errorf("实例不存在: %s", instanceID)
	}

	now := time.Now()
	inst.LastHeart = now
	inst.UpdatedAt = now
	if inst.Status != "running" {
		inst.Status = "running"
	}

	for name, svc := range inst.Services {
		svc.LastHeartbeat = now
		if svc.Status != "running" {
			svc.Status = "running"
		}
		inst.Services[name] = svc
	}

	r.emitEvent(InstanceEvent{Type: "changed", Instance: inst})
	r.persistInstance(inst)
	return nil
}

// HandleEvent 处理实例事件（如 project_opened/project_saved/project_closed）
func (r *InstanceRegistry) HandleEvent(instanceID string, event EventRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.instances[instanceID]
	if !ok {
		return fmt.Errorf("实例不存在: %s", instanceID)
	}

	switch event.EventType {
	case "project_opened", "project_saved":
		if path, ok := event.Data["project_path"].(string); ok {
			inst.ProjectPath = path
		}
	case "project_closed":
		inst.ProjectPath = ""
	}

	inst.UpdatedAt = time.Now()
	r.emitEvent(InstanceEvent{Type: "changed", Instance: inst})
	r.persistInstance(inst)
	r.persistEvent(instanceID, event)

	logger.Info("InstanceRegistry: 实例 %s 事件 %s", instanceID, event.EventType)
	return nil
}

// GetAll 获取所有实例快照
func (r *InstanceRegistry) GetAll() []*QGISInstance {
	r.mu.RLock()
	db := r.db
	result := make([]*QGISInstance, 0, len(r.instances))
	for _, inst := range r.instances {
		cp := *inst
		cp.Services = copyServices(inst.Services)
		result = append(result, &cp)
	}
	r.mu.RUnlock()

	persisted := queryRunningInstances(db)
	if len(persisted) == 0 {
		return result
	}

	if len(result) == 0 {
		return persisted
	}

	merged := make(map[string]*QGISInstance, len(result)+len(persisted))
	for _, inst := range persisted {
		if inst == nil {
			continue
		}
		merged[inst.ID] = inst
	}
	for _, inst := range result {
		if inst == nil {
			continue
		}
		merged[inst.ID] = inst
	}

	combined := make([]*QGISInstance, 0, len(merged))
	for _, inst := range merged {
		combined = append(combined, inst)
	}
	return combined
}

// Get 获取指定实例
func (r *InstanceRegistry) Get(instanceID string) *QGISInstance {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil
	}

	r.mu.RLock()
	db := r.db
	inst, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return queryRunningInstanceByID(db, instanceID)
	}
	cp := *inst
	cp.Services = copyServices(inst.Services)
	return &cp
}

// Events 返回事件通道（供 WebSocket 广播）
func (r *InstanceRegistry) Events() <-chan InstanceEvent {
	return r.eventCh
}

// StartCleanup 启动后台清理协程：检测进程存活 + 心跳超时
func (r *InstanceRegistry) StartCleanup(interval time.Duration) {
	if interval <= 0 {
		interval = defaultCleanupTick
	}

	r.mu.Lock()
	if r.cleanupOn {
		r.mu.Unlock()
		return
	}
	r.cleanupOn = true
	r.mu.Unlock()

	go r.cleanupLoop(interval)
	logger.Info("InstanceRegistry: 清理协程已启动，间隔 %v", interval)
}

// Close 关闭注册表
func (r *InstanceRegistry) Close() {
	select {
	case <-r.stopCh:
		return
	default:
		close(r.stopCh)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db != nil {
		_ = r.db.Close()
		r.db = nil
	}
	logger.Info("InstanceRegistry: 已关闭")
}

func (r *InstanceRegistry) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.cleanup()
		}
	}
}

func (r *InstanceRegistry) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, inst := range r.instances {
		dead := false

		if !IsProcessAlive(inst.PID) {
			dead = true
			logger.Info("InstanceRegistry: 实例 %s 进程已退出 (PID=%d)", id, inst.PID)
		}

		if !dead && !inst.LastHeart.IsZero() {
			if now.Sub(inst.LastHeart) > defaultHeartbeatTTL {
				dead = true
				logger.Info("InstanceRegistry: 实例 %s 心跳超时", id)
			}
		}

		if dead {
			inst.Status = "dead"
			inst.UpdatedAt = now
			delete(r.instances, id)
			r.emitEvent(InstanceEvent{Type: "removed", Instance: inst})
			r.persistInstance(inst)
		}
	}
}

func (r *InstanceRegistry) emitEvent(event InstanceEvent) {
	select {
	case r.eventCh <- event:
	default:
		logger.Warn("InstanceRegistry: 事件通道已满，丢弃事件 type=%s instance=%s", event.Type, event.Instance.ID)
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func queryRunningInstances(db *sql.DB) []*QGISInstance {
	if db == nil {
		return nil
	}

	now := time.Now()

	rows, err := db.Query(`
SELECT
	instance_id,
	machine_fingerprint,
	start_time,
	pid,
	qgis_version,
	project_path,
	status,
	source,
	last_heartbeat_at,
	created_at,
	updated_at
FROM qgis_instances
WHERE status = 'running'
`)
	if err != nil {
		logger.Debug("InstanceRegistry: 查询持久化运行实例失败: %v", err)
		return nil
	}
	defer rows.Close()

	instances := make([]*QGISInstance, 0)
	for rows.Next() {
		inst, scanErr := scanPersistedRunningInstance(rows)
		if scanErr != nil {
			logger.Debug("InstanceRegistry: 扫描持久化运行实例失败: %v", scanErr)
			continue
		}
		if shouldDemotePersistedRunningInstance(inst, now) {
			continue
		}
		instances = append(instances, inst)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		logger.Debug("InstanceRegistry: 遍历持久化运行实例失败: %v", rowsErr)
	}
	return instances
}

func queryRunningInstanceByID(db *sql.DB, instanceID string) *QGISInstance {
	if db == nil || instanceID == "" {
		return nil
	}

	row := db.QueryRow(`
SELECT
	instance_id,
	machine_fingerprint,
	start_time,
	pid,
	qgis_version,
	project_path,
	status,
	source,
	last_heartbeat_at,
	created_at,
	updated_at
FROM qgis_instances
WHERE instance_id = ? AND status = 'running'
LIMIT 1
`, instanceID)

	inst, err := scanPersistedRunningInstance(row)
	if err != nil {
		return nil
	}
	if shouldDemotePersistedRunningInstance(inst, time.Now()) {
		return nil
	}
	return inst
}

func scanPersistedRunningInstance(scanner rowScanner) (*QGISInstance, error) {
	var (
		instanceID, machineFingerprint, startTime string
		qgisVersion, projectPath, status, source  string
		lastHeartbeatRaw                          sql.NullString
		createdRaw, updatedRaw                    string
		pid                                       int
	)

	if err := scanner.Scan(
		&instanceID,
		&machineFingerprint,
		&startTime,
		&pid,
		&qgisVersion,
		&projectPath,
		&status,
		&source,
		&lastHeartbeatRaw,
		&createdRaw,
		&updatedRaw,
	); err != nil {
		return nil, err
	}

	lastHeart := time.Time{}
	if lastHeartbeatRaw.Valid {
		lastHeart = parseTime(lastHeartbeatRaw.String)
	}

	services := buildInstanceServices(source, status, lastHeart)

	inst := &QGISInstance{
		ID:                 instanceID,
		MachineFingerprint: machineFingerprint,
		StartTime:          startTime,
		PID:                pid,
		QGISVersion:        qgisVersion,
		ProjectPath:        projectPath,
		Status:             status,
		Services:           services,
		CreatedAt:          parseTime(createdRaw),
		UpdatedAt:          parseTime(updatedRaw),
		LastHeart:          lastHeart,
		Source:             source,
	}
	return inst, nil
}

func copyServices(src map[string]ServiceInfo) map[string]ServiceInfo {
	dst := make(map[string]ServiceInfo, len(src))
	maps.Copy(dst, src)
	return dst
}

func (r *InstanceRegistry) initSQLite() {
	if isGoTestProcess() {
		r.dbPath = ":memory:"
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			logger.Warn("InstanceRegistry: 打开测试 SQLite 失败: %v", err)
			return
		}
		r.db = db
		if _, schemaErr := db.Exec(`
CREATE TABLE IF NOT EXISTS qgis_instances (
	instance_id TEXT PRIMARY KEY,
	machine_fingerprint TEXT,
	start_time TEXT,
	pid INTEGER NOT NULL,
	qgis_version TEXT,
	project_path TEXT,
	status TEXT NOT NULL,
	source TEXT,
	last_heartbeat_at TEXT,
	connected_at TEXT,
	disconnected_at TEXT,
	created_at TEXT,
	updated_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_qgis_instances_status_heartbeat
	ON qgis_instances(status, last_heartbeat_at);

CREATE TABLE IF NOT EXISTS instance_routes (
	session_id TEXT PRIMARY KEY,
	instance_id TEXT NOT NULL,
	bind_mode TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_instance_routes_instance ON instance_routes(instance_id);

CREATE TABLE IF NOT EXISTS instance_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	payload_json TEXT,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_instance_events_instance_created
	ON instance_events(instance_id, created_at);
`); schemaErr != nil {
			_ = db.Close()
			r.db = nil
			logger.Warn("InstanceRegistry: 初始化测试 SQLite schema 失败: %v", schemaErr)
		}
		return
	}

	dataDir, err := config.ResolvePluginDataDir()
	if err != nil {
		logger.Warn("InstanceRegistry: 获取 AutoCarto 目录失败，禁用 SQLite 持久化: %v", err)
		return
	}

	dbDir := dataDir
	if mkErr := os.MkdirAll(dbDir, 0o755); mkErr != nil {
		logger.Warn("InstanceRegistry: 创建目录失败，禁用 SQLite 持久化: %v", mkErr)
		return
	}

	r.dbPath = filepath.Join(dbDir, "instances.db")
	db, openErr := sql.Open("sqlite3", r.dbPath)
	if openErr != nil {
		logger.Warn("InstanceRegistry: 打开 SQLite 失败: %v", openErr)
		return
	}

	if _, pingErr := db.Exec(`PRAGMA journal_mode=WAL;`); pingErr != nil {
		logger.Debug("InstanceRegistry: WAL 模式设置失败: %v", pingErr)
	}

	if _, schemaErr := db.Exec(`
CREATE TABLE IF NOT EXISTS qgis_instances (
	instance_id TEXT PRIMARY KEY,
	machine_fingerprint TEXT,
	start_time TEXT,
	pid INTEGER NOT NULL,
	qgis_version TEXT,
	project_path TEXT,
	status TEXT NOT NULL,
	source TEXT,
	last_heartbeat_at TEXT,
	connected_at TEXT,
	disconnected_at TEXT,
	created_at TEXT,
	updated_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_qgis_instances_status_heartbeat
	ON qgis_instances(status, last_heartbeat_at);

CREATE TABLE IF NOT EXISTS instance_routes (
	session_id TEXT PRIMARY KEY,
	instance_id TEXT NOT NULL,
	bind_mode TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_instance_routes_instance ON instance_routes(instance_id);

CREATE TABLE IF NOT EXISTS instance_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	payload_json TEXT,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_instance_events_instance_created
	ON instance_events(instance_id, created_at);
`); schemaErr != nil {
		_ = db.Close()
		logger.Warn("InstanceRegistry: 初始化 SQLite schema 失败: %v", schemaErr)
		return
	}

	r.db = db
	logger.Info("InstanceRegistry: SQLite 已启用 %s", r.dbPath)
}

func (r *InstanceRegistry) loadPersistedInstances() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.db == nil {
		return
	}

	rows, err := r.db.Query(`
SELECT
	instance_id,
	machine_fingerprint,
	start_time,
	pid,
	qgis_version,
	project_path,
	status,
	source,
	last_heartbeat_at,
	connected_at,
	created_at,
	updated_at
FROM qgis_instances
WHERE status = 'running'
`)
	if err != nil {
		logger.Warn("InstanceRegistry: 读取持久化实例失败: %v", err)
		return
	}
	defer rows.Close()

	loaded := 0
	now := time.Now()
	for rows.Next() {
		var (
			instanceID, machineFingerprint, startTime string
			qgisVersion, projectPath, status, source  string
			lastHeartbeatRaw, connectedRaw            sql.NullString
			createdRaw, updatedRaw                    string
			pid                                       int
		)
		if scanErr := rows.Scan(
			&instanceID,
			&machineFingerprint,
			&startTime,
			&pid,
			&qgisVersion,
			&projectPath,
			&status,
			&source,
			&lastHeartbeatRaw,
			&connectedRaw,
			&createdRaw,
			&updatedRaw,
		); scanErr != nil {
			logger.Warn("InstanceRegistry: 扫描持久化实例失败: %v", scanErr)
			continue
		}

		lastHeart := time.Time{}
		if lastHeartbeatRaw.Valid {
			lastHeart = parseTime(lastHeartbeatRaw.String)
		}

		services := buildInstanceServices(source, status, lastHeart)

		inst := &QGISInstance{
			ID:                 instanceID,
			MachineFingerprint: machineFingerprint,
			StartTime:          startTime,
			PID:                pid,
			QGISVersion:        qgisVersion,
			ProjectPath:        projectPath,
			Status:             status,
			Services:           services,
			CreatedAt:          parseTime(createdRaw),
			UpdatedAt:          parseTime(updatedRaw),
			LastHeart:          lastHeart,
			Source:             source,
		}

		demoted := shouldDemotePersistedRunningInstance(inst, now)
		if demoted {
			inst.Status = "stopped"
			inst.UpdatedAt = now
			for name, svc := range inst.Services {
				svc.Status = "stopped"
				inst.Services[name] = svc
			}
			logger.Info("InstanceRegistry: 持久化实例状态已降级为 stopped id=%s", instanceID)
			r.persistInstance(inst)
			continue
		}
		r.instances[instanceID] = inst
		loaded++
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		logger.Warn("InstanceRegistry: 遍历持久化实例失败: %v", rowsErr)
		return
	}

	if loaded > 0 {
		logger.Info("InstanceRegistry: 已恢复 %d 条持久化实例记录", loaded)
	}
}

func (r *InstanceRegistry) persistInstance(inst *QGISInstance) {
	if r.db == nil || inst == nil {
		return
	}

	connectedAt := inst.CreatedAt
	if connectedAt.IsZero() {
		connectedAt = time.Now()
	}

	disconnectedAt := ""
	if inst.Status != "running" {
		disconnectedAt = time.Now().Format(time.RFC3339Nano)
	}

	_, err := r.db.Exec(`
INSERT INTO qgis_instances (
	instance_id,
	machine_fingerprint,
	start_time,
	pid,
	qgis_version,
	project_path,
	status,
	source,
	last_heartbeat_at,
	connected_at,
	disconnected_at,
	created_at,
	updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(instance_id) DO UPDATE SET
	machine_fingerprint=excluded.machine_fingerprint,
	start_time=excluded.start_time,
	pid=excluded.pid,
	qgis_version=excluded.qgis_version,
	project_path=excluded.project_path,
	status=excluded.status,
	source=excluded.source,
	last_heartbeat_at=excluded.last_heartbeat_at,
	connected_at=excluded.connected_at,
	disconnected_at=excluded.disconnected_at,
	updated_at=excluded.updated_at
`,
		inst.ID,
		inst.MachineFingerprint,
		inst.StartTime,
		inst.PID,
		inst.QGISVersion,
		inst.ProjectPath,
		inst.Status,
		inst.Source,
		formatTime(inst.LastHeart),
		formatTime(connectedAt),
		disconnectedAt,
		formatTime(inst.CreatedAt),
		formatTime(inst.UpdatedAt),
	)
	if err != nil {
		logger.Warn("InstanceRegistry: 持久化实例失败 id=%s err=%v", inst.ID, err)
	}
}

func buildInstanceServices(source string, status string, lastHeartbeat time.Time) map[string]ServiceInfo {
	services := map[string]ServiceInfo{}
	serviceName := bridgeServiceNameForSource(source)
	services[serviceName] = ServiceInfo{
		Port:          0,
		Status:        strings.TrimSpace(status),
		LastHeartbeat: lastHeartbeat,
	}
	return services
}

func bridgeServiceNameForSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case strings.ToLower(SourceAgentReverseWS):
		return ServiceNameAgentQGISBridge
	default:
		return ServiceNameQGISBridge
	}
}

func (r *InstanceRegistry) persistEvent(instanceID string, event EventRequest) {
	if r.db == nil || instanceID == "" || event.EventType == "" {
		return
	}
	payloadBytes, _ := json.Marshal(event.Data)
	_, err := r.db.Exec(`
INSERT INTO instance_events (instance_id, event_type, payload_json, created_at)
VALUES (?, ?, ?, ?)
`, instanceID, event.EventType, string(payloadBytes), time.Now().Format(time.RFC3339Nano))
	if err != nil {
		logger.Debug("InstanceRegistry: 持久化事件失败 instance=%s event=%s err=%v", instanceID, event.EventType, err)
	}
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339Nano)
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return parsed
	}
	return time.Time{}
}

func isGoTestProcess() bool {
	base := filepath.Base(os.Args[0])
	return strings.Contains(base, ".test")
}

func shouldDemotePersistedRunningInstance(inst *QGISInstance, now time.Time) bool {
	if inst == nil || inst.Status != "running" {
		return false
	}

	if inst.PID <= 0 || !IsProcessAlive(inst.PID) {
		return true
	}

	if inst.LastHeart.IsZero() {
		return true
	}

	return now.Sub(inst.LastHeart) > defaultHeartbeatTTL
}
