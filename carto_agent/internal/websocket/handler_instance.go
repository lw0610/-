package websocket

import (
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/gorilla/websocket"
)

func (s *DeepServer) handleInstanceMessage(conn *websocket.Conn, msg Message) bool {
	switch msg.Type {
	case MsgTypeInstanceList:
		s.handleInstanceList(conn)
	case MsgTypeInstanceSelect:
		s.handleInstanceSelect(conn, msg)
	default:
		return false
	}
	return true
}

func (s *DeepServer) handleInstanceList(conn *websocket.Conn) {
	snapshot := s.GetRuntimeSnapshot()
	items := snapshot.Instances.Items
	if items == nil {
		items = []*discovery.QGISInstance{}
	}

	s.sendMessage(conn, Message{
		Type: MsgTypeInstanceList,
		Data: map[string]any{
			"instances":      items,
			"active_id":      snapshot.Instances.ActiveID,
			"qgis_available": snapshot.Instances.RunningCount > 0,
			"instance_count": snapshot.Instances.InstanceCount,
			"running_count":  snapshot.Instances.RunningCount,
			"stale":          snapshot.Stale,
		},
	})
}

func (s *DeepServer) handleInstanceSelect(conn *websocket.Conn, msg Message) {
	instanceID, _ := msg.Data["instance_id"].(string)
	instanceID = strings.TrimSpace(instanceID)
	requestID, _ := msg.Data["request_id"].(string)
	requestID = strings.TrimSpace(requestID)
	currentActiveID := strings.TrimSpace(s.GetRuntimeSnapshot().Instances.ActiveID)

	if instanceID == "" {
		s.sendInstanceSelectResult(conn, requestID, currentActiveID, nil, "缺少 instance_id 参数")
		return
	}
	if s != nil {
		s.mu.Lock()
		localBridgeProvider := s.localBridgeProvider
		s.mu.Unlock()
		if localBridgeProvider != nil && localBridgeProvider.HasInstanceConnected(instanceID) {
			localBridgeProvider.SetActiveInstance(instanceID)
			instance, _ := findRunningInstanceByID(s.GetRuntimeSnapshot().Instances.Items, instanceID)
			s.sendInstanceSelectResult(conn, requestID, instanceID, instance, "")
			return
		}
	}
	s.sendInstanceSelectResult(conn, requestID, currentActiveID, nil, "实例桥接连接未就绪")
}

func (s *DeepServer) sendInstanceSelectResult(conn *websocket.Conn, requestID, activeID string, instance *discovery.QGISInstance, errMsg string) {
	data := map[string]any{
		"active_id":    strings.TrimSpace(activeID),
		"request_id":   strings.TrimSpace(requestID),
		"confirmed_at": time.Now().UTC().Format(time.RFC3339),
		"success":      strings.TrimSpace(errMsg) == "",
	}
	if instance != nil {
		data["instance"] = instance
	}
	if strings.TrimSpace(errMsg) != "" {
		data["error"] = strings.TrimSpace(errMsg)
	}
	_ = s.sendMessage(conn, Message{Type: MsgTypeInstanceChanged, Data: data})
}
