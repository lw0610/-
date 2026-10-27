package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/toolsession"
)

func tileArgs(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{"definition_path": filepath.Join(t.TempDir(), "tiles.layer.json"), "confirmed": true}
}
func tileContext(t *testing.T, args map[string]any, instance string) context.Context {
	t.Helper()
	ctx, err := AuthorizeConfirmedTileLoad(toolsession.WithContext(context.Background(), "fixture-session", "", instance), args)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestPrivateTileLoadRequiresBoundSystemCapability(t *testing.T) {
	p, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()
	sendInstanceConnectWithExtras(t, conn, "inst-1", nil)
	waitForInstanceConnected(t, p, "inst-1")
	loads := 0
	p.loadTileKey = func() (string, error) { loads++; return "fake-key-never-returned", nil }
	args := tileArgs(t)
	bound := toolsession.WithContext(context.Background(), "fixture-session", "", "inst-1")
	if _, err := p.InvokeTool(bound, "data_load_tiles", args); err == nil {
		t.Fatal("JSON confirmed flag supplied authority")
	}
	ctx := tileContext(t, args, "inst-1")
	changed := map[string]any{"definition_path": args["definition_path"], "confirmed": true, "target_instance_id": "inst-2"}
	if _, err := p.InvokeTool(ctx, "data_load_tiles", changed); err == nil {
		t.Fatal("retargeted instance accepted")
	}
	changed = map[string]any{"definition_path": filepath.Join(t.TempDir(), "different.json"), "confirmed": true}
	if _, err := p.InvokeTool(ctx, "data_load_tiles", changed); err == nil {
		t.Fatal("different definition accepted")
	}
	changed = map[string]any{"definition_path": args["definition_path"], "confirmed": true, privateCredentialsKey: map[string]any{"tianditu": "caller-secret"}}
	if _, err := p.InvokeTool(ctx, "data_load_tiles", changed); err == nil {
		t.Fatal("caller credentials accepted")
	}
	cancelled, cancel := context.WithCancel(tileContext(t, args, "inst-1"))
	cancel()
	if _, err := p.InvokeTool(cancelled, "data_load_tiles", args); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request was dispatched", err)
	}
	if loads != 0 {
		t.Fatal("credentials read before authorization", loads)
	}
}

func TestPrivateTileBridgeInjectsOnlyFinalCopyAndRedactsEveryResultField(t *testing.T) {
	p, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()
	sendInstanceConnectWithExtras(t, conn, "inst-1", nil)
	waitForInstanceConnected(t, p, "inst-1")
	fakeKey := "fake-tianditu-key/&?=+"
	loads := 0
	p.loadTileKey = func() (string, error) { loads++; return fakeKey, nil }
	args := tileArgs(t)
	args["authcfg"] = "abc1234" // Current authcfg support never bypasses the Key requirement.
	original, _ := json.Marshal(args)
	ctx := tileContext(t, args, "inst-1")
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	wire := make(chan map[string]any, 1)
	go func() {
		msg := readWSMessage(t, conn)
		params, _ := msg.Data["params"].(map[string]any)
		wire <- params
		request, _ := msg.Data["request_id"].(string)
		sendWSMessage(t, conn, testWSMessage{Type: "tool_result", Data: map[string]any{
			"request_id": request, "success": true, "output": map[string]any{"source": "https://tiles.test?tk=" + url.QueryEscape(fakeKey), "nested": []any{map[string]any{"source": fakeKey, privateCredentialsKey: map[string]any{"tianditu": fakeKey}}}},
			"error": fakeKey, "message": fakeKey, "output_layers": []map[string]any{{"path": fakeKey, "auto_load": true}}, "provider_diagnostics": map[string]any{"detail": fakeKey},
		}})
	}()
	result, err := p.InvokeTool(ctx, "data_load_tiles", args)
	if err != nil {
		t.Fatal(err)
	}
	params := <-wire
	credentials, _ := params[privateCredentialsKey].(map[string]any)
	if credentials["tianditu"] != fakeKey || params["target_instance_id"] != "inst-1" || loads != 1 {
		t.Fatal("private final injection missing")
	}
	current, _ := json.Marshal(args)
	if string(current) != string(original) || strings.Contains(string(current), fakeKey) || args[privateCredentialsKey] != nil {
		t.Fatal("caller/BaseTool args mutated")
	}
	raw, _ := json.Marshal(result)
	for _, forbidden := range []string{fakeKey, url.QueryEscape(fakeKey), privateCredentialsKey} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("credential escaped the bridge result boundary")
		}
	}
	if !result.Success || !strings.Contains(string(raw), "[redacted]") {
		t.Fatal("redaction damaged result structure")
	}
	if _, err := p.InvokeTool(ctx, "data_load_tiles", args); err == nil || loads != 1 {
		t.Fatal("single-use authorization replayed")
	}
}

func TestPrivateTileLoadDoesNotReadCredentialForDisconnectedInstance(t *testing.T) {
	p, _, cleanup := setupBridgeProviderTest(t)
	defer cleanup()
	loaded := false
	p.loadTileKey = func() (string, error) { loaded = true; return "fake-key", nil }
	args := tileArgs(t)
	if _, err := p.InvokeTool(tileContext(t, args, "missing-instance"), "data_load_tiles", args); err == nil || loaded {
		t.Fatal("credential read without live bound instance")
	}
}

func TestPrivateTileCredentialErrorDoesNotExposeLoaderMessage(t *testing.T) {
	p, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()
	sendInstanceConnectWithExtras(t, conn, "inst-1", nil)
	waitForInstanceConnected(t, p, "inst-1")
	p.loadTileKey = func() (string, error) { return "fake-secret", errors.New("fake-secret storage failure") }
	args := tileArgs(t)
	_, err := p.InvokeTool(tileContext(t, args, "inst-1"), "data_load_tiles", args)
	if err == nil || strings.Contains(err.Error(), "fake-secret") {
		t.Fatal("unsafe key-loader error")
	}
}

func TestPrivateTileAuthorizationRejectsPublicSecretArguments(t *testing.T) {
	bound := toolsession.WithContext(context.Background(), "fixture-session", "", "inst-1")
	for _, args := range []map[string]any{
		{"definition_path": "relative.json", "confirmed": true},
		{"definition_path": "/tmp/tiles.json", "confirmed": false},
		{"definition_path": "/tmp/tiles.json", "confirmed": "true"},
		{"definition_path": "/tmp/tiles.json", "confirmed": true, "key": "fake-secret"},
		{"definition_path": "/tmp/tiles.json", "confirmed": true, "authcfg": "https://example.test?tk=fake-secret"},
	} {
		if _, err := AuthorizeConfirmedTileLoad(bound, args); err == nil {
			t.Fatal("invalid public arguments accepted")
		}
	}
	if _, err := AuthorizeConfirmedTileLoad(context.Background(), tileArgs(t)); err == nil {
		t.Fatal("unbound session accepted")
	}
}
