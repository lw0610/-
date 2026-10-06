package discovery

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestServiceRegistry(t *testing.T) (*ServiceRegistry, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "instances.db")
	registry, err := NewServiceRegistry(dbPath)
	if err != nil {
		t.Fatalf("create service registry: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := registry.Close(); closeErr != nil {
			t.Errorf("close service registry: %v", closeErr)
		}
	})

	return registry, dbPath
}

func testServiceEntry(serviceName string, port int) ServicePortEntry {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return ServicePortEntry{
		ServiceName:  serviceName,
		Host:         "127.0.0.1",
		Port:         port,
		Protocol:     "http",
		PID:          os.Getpid(),
		Status:       "running",
		RegisteredAt: now,
		LastSeen:     now,
	}
}

func TestServiceNewServiceRegistry(t *testing.T) {
	registry, dbPath := newTestServiceRegistry(t)

	if registry.db == nil {
		t.Fatalf("expected sqlite db to be initialized")
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db file to exist: %v", err)
	}

	row := registry.db.QueryRow(`
SELECT name
FROM sqlite_master
WHERE type = 'table' AND name = 'service_ports'
LIMIT 1
`)

	var tableName string
	if err := row.Scan(&tableName); err != nil {
		t.Fatalf("expected service_ports table to exist: %v", err)
	}
	if tableName != "service_ports" {
		t.Fatalf("unexpected table name: got=%s", tableName)
	}
}

func TestServiceRegister(t *testing.T) {
	tests := []struct {
		name  string
		entry ServicePortEntry
	}{
		{
			name:  "agent websocket",
			entry: testServiceEntry("agent", 3000),
		},
		{
			name: "agent qgis bridge",
			entry: ServicePortEntry{
				ServiceName:  ServiceNameAgentQGISBridge,
				Host:         "127.0.0.1",
				Port:         3000,
				Protocol:     "websocket",
				PID:          os.Getpid(),
				Status:       "running",
				RegisteredAt: time.Now().UTC().Format(time.RFC3339Nano),
				LastSeen:     time.Now().UTC().Format(time.RFC3339Nano),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, _ := newTestServiceRegistry(t)

			if err := registry.Register(tt.entry); err != nil {
				t.Fatalf("register service: %v", err)
			}

			entry, err := registry.GetPort(tt.entry.ServiceName)
			if err != nil {
				t.Fatalf("get service port: %v", err)
			}

			if entry.ServiceName != tt.entry.ServiceName {
				t.Fatalf("service name mismatch: got=%s want=%s", entry.ServiceName, tt.entry.ServiceName)
			}
			if entry.Host != tt.entry.Host {
				t.Fatalf("host mismatch: got=%s want=%s", entry.Host, tt.entry.Host)
			}
			if entry.Port != tt.entry.Port {
				t.Fatalf("port mismatch: got=%d want=%d", entry.Port, tt.entry.Port)
			}
			if entry.Protocol != tt.entry.Protocol {
				t.Fatalf("protocol mismatch: got=%s want=%s", entry.Protocol, tt.entry.Protocol)
			}
			if entry.PID != tt.entry.PID {
				t.Fatalf("pid mismatch: got=%d want=%d", entry.PID, tt.entry.PID)
			}
			if entry.Status != tt.entry.Status {
				t.Fatalf("status mismatch: got=%s want=%s", entry.Status, tt.entry.Status)
			}
		})
	}
}

func TestServiceRegisterUpsert(t *testing.T) {
	tests := []struct {
		name   string
		first  ServicePortEntry
		second ServicePortEntry
	}{
		{
			name:   "overwrite existing service entry",
			first:  testServiceEntry("middleware", 50100),
			second: testServiceEntry("middleware", 50123),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, _ := newTestServiceRegistry(t)
			tt.second.Protocol = "http-jsonrpc"

			if err := registry.Register(tt.first); err != nil {
				t.Fatalf("register first entry: %v", err)
			}
			if err := registry.Register(tt.second); err != nil {
				t.Fatalf("register second entry: %v", err)
			}

			entry, err := registry.GetPort(tt.second.ServiceName)
			if err != nil {
				t.Fatalf("get upserted entry: %v", err)
			}

			if entry.Port != tt.second.Port {
				t.Fatalf("port mismatch after upsert: got=%d want=%d", entry.Port, tt.second.Port)
			}
			if entry.Protocol != tt.second.Protocol {
				t.Fatalf("protocol mismatch after upsert: got=%s want=%s", entry.Protocol, tt.second.Protocol)
			}
		})
	}
}

func TestServiceUnregister(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
	}{
		{
			name:        "remove registered service",
			serviceName: "compute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, _ := newTestServiceRegistry(t)
			if err := registry.Register(testServiceEntry(tt.serviceName, 50301)); err != nil {
				t.Fatalf("register service: %v", err)
			}

			if err := registry.Unregister(tt.serviceName); err != nil {
				t.Fatalf("unregister service: %v", err)
			}

			_, err := registry.GetPort(tt.serviceName)
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("expected sql.ErrNoRows after unregister, got=%v", err)
			}
		})
	}
}

func TestServiceGetAllPorts(t *testing.T) {
	registry, _ := newTestServiceRegistry(t)

	entries := []ServicePortEntry{
		testServiceEntry("agent", 3000),
		testServiceEntry("middleware", 50100),
		testServiceEntry("compute", 50301),
	}

	for _, entry := range entries {
		if err := registry.Register(entry); err != nil {
			t.Fatalf("register service %s: %v", entry.ServiceName, err)
		}
	}

	allEntries, err := registry.GetAllPorts()
	if err != nil {
		t.Fatalf("get all service ports: %v", err)
	}
	if len(allEntries) != len(entries) {
		t.Fatalf("entry count mismatch: got=%d want=%d", len(allEntries), len(entries))
	}

	ports := make(map[string]int, len(allEntries))
	for _, entry := range allEntries {
		ports[entry.ServiceName] = entry.Port
	}

	for _, entry := range entries {
		if got := ports[entry.ServiceName]; got != entry.Port {
			t.Fatalf("service %s port mismatch: got=%d want=%d", entry.ServiceName, got, entry.Port)
		}
	}
}

func TestServiceHeartbeat(t *testing.T) {
	registry, _ := newTestServiceRegistry(t)

	registeredAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	lastSeen := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	entry := ServicePortEntry{
		ServiceName:  "agent",
		Host:         "127.0.0.1",
		Port:         3000,
		Protocol:     "websocket",
		PID:          os.Getpid(),
		Status:       "running",
		RegisteredAt: registeredAt,
		LastSeen:     lastSeen,
	}

	if err := registry.Register(entry); err != nil {
		t.Fatalf("register service: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := registry.Heartbeat(entry.ServiceName); err != nil {
		t.Fatalf("heartbeat service: %v", err)
	}

	updated, err := registry.GetPort(entry.ServiceName)
	if err != nil {
		t.Fatalf("get service after heartbeat: %v", err)
	}

	previousTime, err := time.Parse(time.RFC3339Nano, lastSeen)
	if err != nil {
		t.Fatalf("parse previous heartbeat: %v", err)
	}
	updatedTime, err := time.Parse(time.RFC3339Nano, updated.LastSeen)
	if err != nil {
		t.Fatalf("parse updated heartbeat: %v", err)
	}
	if !updatedTime.After(previousTime) {
		t.Fatalf("expected heartbeat to refresh last_seen: previous=%s updated=%s", previousTime, updatedTime)
	}
}

func TestServiceCleanExpired(t *testing.T) {
	registry, _ := newTestServiceRegistry(t)

	now := time.Now().UTC()
	entries := []ServicePortEntry{
		{
			ServiceName:  "middleware",
			Host:         "127.0.0.1",
			Port:         50100,
			Protocol:     "http",
			PID:          os.Getpid(),
			Status:       "running",
			RegisteredAt: now.Add(-time.Hour).Format(time.RFC3339Nano),
			LastSeen:     now.Add(-time.Hour).Format(time.RFC3339Nano),
		},
		{
			ServiceName:  "agent",
			Host:         "127.0.0.1",
			Port:         3000,
			Protocol:     "websocket",
			PID:          os.Getpid(),
			Status:       "running",
			RegisteredAt: now.Format(time.RFC3339Nano),
			LastSeen:     now.Format(time.RFC3339Nano),
		},
	}

	for _, entry := range entries {
		if err := registry.Register(entry); err != nil {
			t.Fatalf("register service %s: %v", entry.ServiceName, err)
		}
	}

	removed, err := registry.CleanExpired(5 * time.Minute)
	if err != nil {
		t.Fatalf("clean expired services: %v", err)
	}
	if removed != 1 {
		t.Fatalf("unexpected removed count: got=%d want=1", removed)
	}

	_, err = registry.GetPort("middleware")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected middleware to be removed, got=%v", err)
	}

	if _, err := registry.GetPort("agent"); err != nil {
		t.Fatalf("expected fresh agent entry to remain: %v", err)
	}
}
