package runtimepersist

import (
	"context"
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/cloudwego/eino/adk"
)

func init() {
	// Eino v0.8 stores this map in State's interface-valued internal fields.
	gob.Register(map[string]*adk.AgentAction{})
}

const checkpointStoreSchema = `
CREATE TABLE IF NOT EXISTS runtime_checkpoints (
	checkpoint_id TEXT PRIMARY KEY,
	payload BLOB NOT NULL,
	runtime_revision TEXT NOT NULL DEFAULT '',
	active_config_name TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_runtime_checkpoints_updated_at
ON runtime_checkpoints(updated_at);
`

// StoreMetadata 描述当前运行时拓扑，用于 checkpoint 绑定。
type StoreMetadata struct {
	RuntimeRevision string
	ActiveConfig    string
}

// MetadataProvider 返回当前运行时元信息。
type MetadataProvider func() StoreMetadata

// CheckpointStore 在 ADK CheckPointStore 基础上补充删除能力。
type CheckpointStore interface {
	adk.CheckPointStore
	Delete(ctx context.Context, checkPointID string) error
}

// ResolveCheckpointStorePath 计算 checkpoint sqlite 文件路径。
func ResolveCheckpointStorePath(configBaseDir string) string {
	trimmed := filepath.Clean(strings.TrimSpace(configBaseDir))
	if trimmed != "" && filepath.Base(trimmed) == "config" {
		return filepath.Join(filepath.Dir(trimmed), "data", "runtime", "checkpoint_store.db")
	}
	if dataDir, err := config.ResolvePluginDataDir(); err == nil {
		return filepath.Join(dataDir, "runtime", "checkpoint_store.db")
	}
	return filepath.Join(trimmed, "runtime", "checkpoint_store.db")
}

type checkpointRecord struct {
	payload         []byte
	runtimeRevision string
}

// ErrCheckpointRevisionMismatch 表示 checkpoint 所属 runtime 拓扑已变更。
type ErrCheckpointRevisionMismatch struct {
	StoredRevision  string
	CurrentRevision string
}

func (e *ErrCheckpointRevisionMismatch) Error() string {
	if e == nil {
		return "checkpoint runtime revision mismatch"
	}
	return fmt.Sprintf("checkpoint runtime revision mismatch: stored=%s current=%s", e.StoredRevision, e.CurrentRevision)
}

func validateCheckpointRevision(recordRevision string, metadata StoreMetadata) error {
	stored := recordRevision
	current := metadata.RuntimeRevision
	if stored == "" || current == "" || stored == current {
		return nil
	}
	return &ErrCheckpointRevisionMismatch{
		StoredRevision:  stored,
		CurrentRevision: current,
	}
}

func currentMetadata(provider MetadataProvider) StoreMetadata {
	if provider == nil {
		return StoreMetadata{}
	}
	return provider()
}

// MemoryCheckpointStore 内存实现，供测试和未持久化场景使用。
type MemoryCheckpointStore struct {
	mu               sync.RWMutex
	data             map[string]checkpointRecord
	metadataProvider MetadataProvider
}

// NewMemoryCheckpointStore 创建内存 checkpoint store。
func NewMemoryCheckpointStore(metadataProvider MetadataProvider) *MemoryCheckpointStore {
	return &MemoryCheckpointStore{
		data:             make(map[string]checkpointRecord),
		metadataProvider: metadataProvider,
	}
}

// Get 获取 checkpoint。
func (s *MemoryCheckpointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	record, ok := s.data[checkPointID]
	s.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}

	if err := validateCheckpointRevision(record.runtimeRevision, currentMetadata(s.metadataProvider)); err != nil {
		return nil, false, err
	}

	cp := make([]byte, len(record.payload))
	copy(cp, record.payload)
	return cp, true, nil
}

// Set 保存 checkpoint。
func (s *MemoryCheckpointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := make([]byte, len(checkPoint))
	copy(cp, checkPoint)
	s.data[checkPointID] = checkpointRecord{
		payload:         cp,
		runtimeRevision: currentMetadata(s.metadataProvider).RuntimeRevision,
	}
	return nil
}

// Delete 删除 checkpoint。
func (s *MemoryCheckpointStore) Delete(_ context.Context, checkPointID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, checkPointID)
	return nil
}

// SQLiteCheckpointStore 持久化 checkpoint store。
type SQLiteCheckpointStore struct {
	db               *sql.DB
	metadataProvider MetadataProvider
}

// NewSQLiteCheckpointStore 创建 sqlite checkpoint store。
func NewSQLiteCheckpointStore(dbPath string, metadataProvider MetadataProvider) (*SQLiteCheckpointStore, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("checkpoint db path is required")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create checkpoint store dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open checkpoint db: %w", err)
	}

	store := &SQLiteCheckpointStore{
		db:               db,
		metadataProvider: metadataProvider,
	}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteCheckpointStore) init() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite checkpoint store is not initialized")
	}

	stmts := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		checkpointStoreSchema,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init checkpoint store: %w", err)
		}
	}
	return nil
}

// Get 获取 checkpoint。
func (s *SQLiteCheckpointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("sqlite checkpoint store is not initialized")
	}

	var payload []byte
	var revision string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT payload, runtime_revision FROM runtime_checkpoints WHERE checkpoint_id = ?`,
		checkPointID,
	).Scan(&payload, &revision)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	if err := validateCheckpointRevision(revision, currentMetadata(s.metadataProvider)); err != nil {
		return nil, false, err
	}

	cp := make([]byte, len(payload))
	copy(cp, payload)
	return cp, true, nil
}

// Set 保存 checkpoint。
func (s *SQLiteCheckpointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite checkpoint store is not initialized")
	}

	meta := currentMetadata(s.metadataProvider)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO runtime_checkpoints (
			checkpoint_id, payload, runtime_revision, active_config_name, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(checkpoint_id) DO UPDATE SET
			payload = excluded.payload,
			runtime_revision = excluded.runtime_revision,
			active_config_name = excluded.active_config_name,
			updated_at = excluded.updated_at`,
		checkPointID,
		checkPoint,
		meta.RuntimeRevision,
		meta.ActiveConfig,
		now,
		now,
	)
	return err
}

// Delete 删除 checkpoint。
func (s *SQLiteCheckpointStore) Delete(ctx context.Context, checkPointID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite checkpoint store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM runtime_checkpoints WHERE checkpoint_id = ?`, checkPointID)
	return err
}

// Close 关闭 sqlite 连接。
func (s *SQLiteCheckpointStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
