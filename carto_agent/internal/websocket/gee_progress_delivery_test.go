package websocket

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/gorilla/websocket"
)

// Simulate a stalled write without depending on kernel socket buffer sizes.
type geeProgressTestConn struct {
	net.Conn
	blocked  bool
	closed   bool
	deadline time.Time
}

func (c *geeProgressTestConn) SetWriteDeadline(deadline time.Time) error {
	c.deadline = deadline
	return c.Conn.SetWriteDeadline(deadline)
}

func (c *geeProgressTestConn) Write(data []byte) (int, error) {
	if c.blocked {
		wait := 2 * time.Second // bounded even if the implementation loses its deadline
		if !c.deadline.IsZero() {
			wait = max(time.Duration(0), time.Until(c.deadline))
		}
		time.Sleep(wait)
		return 0, os.ErrDeadlineExceeded
	}
	return c.Conn.Write(data)
}

func (c *geeProgressTestConn) Close() error {
	c.closed = true
	return c.Conn.Close()
}

func newGEEProgressTestConnection(t *testing.T) (*websocket.Conn, *websocket.Conn, *geeProgressTestConn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := websocket.Upgrader{}
		conn, err := u.Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	}))
	t.Cleanup(server.Close)
	var transport *geeProgressTestConn
	dialer := websocket.Dialer{NetDial: func(network, address string) (net.Conn, error) {
		conn, err := net.Dial(network, address)
		if err != nil {
			return nil, err
		}
		transport = &geeProgressTestConn{Conn: conn}
		return transport, nil
	}}
	client, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	peer := <-accepted
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close() })
	return client, peer, transport
}

func TestGEEProgressStalledWriteIsBounded(t *testing.T) {
	conn, _, transport := newGEEProgressTestConnection(t)
	transport.blocked = true
	server := NewDeepServer(0, nil)
	started := time.Now()
	err := server.writeGEEProgressFrames(conn, [][]byte{[]byte("progress")})
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("expected a bounded write timeout, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("stalled progress write blocked cleanup")
	}
	if !transport.closed {
		t.Fatal("timed out websocket was left open and unusable")
	}
}

func TestGEEProgressDoesNotLeakDeadlineToOrdinaryMessages(t *testing.T) {
	conn, peer, transport := newGEEProgressTestConnection(t)
	server := NewDeepServer(0, nil)
	if err := server.writeGEEProgressFrames(conn, [][]byte{[]byte("progress")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := peer.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err := server.sendMessage(conn, Message{Type: MsgTypeStatus, Content: "ordinary"}); err != nil {
		t.Fatal(err)
	}
	if !transport.deadline.IsZero() {
		t.Fatal("short progress deadline leaked into ordinary message delivery")
	}
	if _, _, err := peer.ReadMessage(); err != nil {
		t.Fatal(err)
	}
}

func TestGEEProgressBusyConnectionStillPersistsProjection(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	conn, _, _ := newGEEProgressTestConnection(t)
	server := NewDeepServer(0, nil)
	projectPath := filepath.Join(t.TempDir(), "project.qgz")
	const sessionID = "gee-busy-connection"
	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID, "name": "GEE progress", "messages": []any{},
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatal(err)
	}
	server.markSessionRuntimeRunning(sessionID, "run-gee", executionModeBuild, "building", projectPath, "")
	writeMu := server.getConnWriteMu(conn)
	writeMu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- server.sendSessionMessage(conn, Message{
			Type: MsgTypeAgentWorkflow, SessionID: sessionID, Phase: "progress",
			Content: "GEE 正在下载 image.tif：25%",
			Data:    map[string]any{"source": "gee", "request_key": "run-gee/1", "percent": 25},
		})
	}()
	select {
	case err := <-done:
		writeMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		writeMu.Unlock()
		<-done
		t.Fatal("progress waited for the connection write lock")
	}
	baseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := runtimepersist.LoadSessionSnapshotJSON(filepath.Join(baseDir, sessionID, runtimeSessionSnapshotFileName))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RuntimeProjection == nil || !strings.Contains(snapshot.RuntimeProjection.RuntimeSummary, "25%") {
		t.Fatalf("dropped progress was not persisted: %+v", snapshot.RuntimeProjection)
	}
}
