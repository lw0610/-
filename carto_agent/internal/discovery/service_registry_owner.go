package discovery

import (
	"fmt"
	"strings"
	"time"
)

// UnregisterOwned never removes a replacement backend's registration. The owner
// predicate is part of the SQL operation, so no read/delete race is possible.
func (r *ServiceRegistry) UnregisterOwned(serviceName string, pid, port int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db == nil {
		return fmt.Errorf("服务注册表未初始化")
	}
	_, err := r.db.Exec(`DELETE FROM service_ports WHERE service_name = ? AND pid = ? AND port = ?`, strings.TrimSpace(serviceName), pid, port)
	return err
}

// HeartbeatOwned prevents a retiring backend from refreshing a newer record.
func (r *ServiceRegistry) HeartbeatOwned(serviceName string, pid, port int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db == nil {
		return fmt.Errorf("服务注册表未初始化")
	}
	_, err := r.db.Exec(`UPDATE service_ports SET last_seen = ? WHERE service_name = ? AND pid = ? AND port = ?`, time.Now().UTC().Format(time.RFC3339Nano), strings.TrimSpace(serviceName), pid, port)
	return err
}
