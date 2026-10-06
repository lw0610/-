package browserruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxSessions = 8
const idleTimeout = 15 * time.Minute

var DefaultManager = NewManager()

type browserSession struct {
	gate        chan struct{}
	session     *mcp.ClientSession
	connection  mcp.Connection
	command     *exec.Cmd
	cancel      context.CancelFunc
	fingerprint string
	stderr      *boundedBuffer
	dead        atomic.Bool
	halt        atomic.Bool
	stopMu      sync.Mutex
	stop        context.CancelFunc
	refs        int       // protected by Manager.mu
	lastUsed    time.Time // protected by Manager.mu
}

// Manager owns child processes. Calls within one chat are serialized; different
// chats use different processes and browser contexts. Call CloseAll on shutdown.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*browserSession
	closed   bool
}

func NewManager() *Manager { return &Manager{sessions: make(map[string]*browserSession)} }

func Invoke(ctx context.Context, name string, args map[string]any, opts Options) (any, error) {
	return DefaultManager.Invoke(ctx, name, args, opts)
}

func (m *Manager) Invoke(ctx context.Context, name string, args map[string]any, opts Options) (any, error) {
	if !Handles(name) {
		return nil, fmt.Errorf("未知浏览器入口 %q", name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Closing must remain possible after configuration has changed or disabled.
	if name == "browser_close" || !opts.Enabled {
		m.closeSession(strings.TrimSpace(opts.SessionID))
		if name == "browser_close" {
			return map[string]any{"closed": true, "session_id": opts.SessionID}, nil
		}
		if name != "browser_check_environment" {
			return nil, errors.New("浏览器在线取数未启用，请先在设置中启用浏览器")
		}
	}
	var err error
	opts, err = normalize(opts)
	if err != nil {
		return nil, err
	}
	if name == "browser_check_environment" {
		return checkEnvironment(opts), nil
	}
	if opts.SessionID == "" {
		return nil, errors.New("浏览器操作缺少当前会话标识")
	}
	if opts.OutputDir, err = prepareOutputDir(opts.OutputDir); err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, opts.timeout())
	defer cancel()
	e, err := m.acquire(opts.SessionID)
	if err != nil {
		return nil, err
	}
	defer m.release(opts.SessionID, e)
	select {
	case <-e.gate:
	case <-callCtx.Done():
		return nil, callCtx.Err()
	}
	defer func() { e.gate <- struct{}{} }()
	if err := callCtx.Err(); err != nil {
		return nil, err
	}
	if e.halt.Load() {
		return nil, errors.New("浏览器会话正在关闭，请重试")
	}
	if err := e.connect(callCtx, opts); err != nil {
		return nil, err
	}
	var result any
	switch name {
	case "browser_list_tools":
		result, err = listTools(callCtx, e.session, opts.OutputDir)
	case "browser_call_tool":
		toolName, _ := args["tool_name"].(string)
		toolName = strings.TrimSpace(toolName)
		if toolName == "" || !strings.HasPrefix(toolName, "browser_") {
			return nil, errors.New("tool_name 必须是 browser_list_tools 返回的 browser_ 工具名")
		}
		arguments := map[string]any{}
		if raw, exists := args["args"]; exists && raw != nil {
			var ok bool
			arguments, ok = raw.(map[string]any)
			if !ok {
				return nil, errors.New("browser_call_tool 的 args 必须是 JSON 对象")
			}
		}
		// Empty objects can arrive as typed-nil maps after skill parameter
		// cloning. The SDK's interface-nil check does not catch those, and
		// encoding/json would send arguments:null instead of the required {}.
		if arguments == nil {
			arguments = map[string]any{}
		}
		if err = validateToolArguments(arguments, opts.OutputDir); err != nil {
			return nil, err
		}
		var reply *mcp.CallToolResult
		reply, err = e.session.CallTool(callCtx, &mcp.CallToolParams{Name: toolName, Arguments: arguments})
		if err == nil {
			result, err = readableResult(reply, opts.OutputDir)
			if err == nil && reply.IsError {
				return result, fmt.Errorf("浏览器工具 %s 执行失败，浏览器会话已保留", toolName)
			}
		}
	}
	if err != nil {
		if callCtx.Err() != nil {
			e.close()
			return nil, fmt.Errorf("浏览器操作已取消或超时，浏览器会话已关闭: %w", callCtx.Err())
		}
		// A structured JSON-RPC error rejects this request, not the session.
		// Keep the open page so bad parameters or a failed action can be fixed
		// without reopening the browser and repeating previous navigation.
		var rpcErr *jsonrpc.Error
		if errors.As(err, &rpcErr) && rpcErr.Code != -32003 && rpcErr.Code != -32004 {
			return nil, fmt.Errorf("浏览器 MCP 请求失败，浏览器会话已保留: %w", err)
		}
		// The SDK's -32003/-32004 codes mean client/server closing; other
		// unstructured errors indicate a broken transport or invalid response.
		diagnostic := e.stderr.String()
		e.close()
		return nil, fmt.Errorf("浏览器 MCP 调用失败，会话已关闭: %w%s", err, diagnosticSuffix(diagnostic))
	}
	return result, nil
}

func (m *Manager) acquire(id string) (*browserSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("浏览器运行服务已关闭")
	}
	now := time.Now()
	for key, e := range m.sessions {
		if e.refs == 0 && (e.dead.Load() || now.Sub(e.lastUsed) >= idleTimeout) {
			select {
			case <-e.gate:
				e.close()
				e.gate <- struct{}{}
				delete(m.sessions, key)
			default:
			}
		}
	}
	e := m.sessions[id]
	if e == nil {
		if len(m.sessions) >= maxSessions {
			return nil, fmt.Errorf("浏览器最多同时保留 %d 个会话，请关闭不再使用的浏览器后重试", maxSessions)
		}
		e = &browserSession{gate: make(chan struct{}, 1), lastUsed: now}
		e.dead.Store(true)
		e.gate <- struct{}{}
		m.sessions[id] = e
	}
	e.refs++
	e.lastUsed = now
	return e, nil
}

func (m *Manager) release(id string, e *browserSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.refs--
	e.lastUsed = time.Now()
	if e.refs == 0 && e.dead.Load() && m.sessions[id] == e {
		delete(m.sessions, id)
	}
}

func (m *Manager) closeSession(id string) {
	m.mu.Lock()
	e := m.sessions[id]
	if e != nil {
		e.refs++
		e.halt.Store(true)
	}
	m.mu.Unlock()
	if e == nil {
		return
	}
	select {
	case <-e.gate:
	default:
		e.requestStop()
		<-e.gate
	}
	e.close()
	e.gate <- struct{}{}
	m.release(id, e)
}

// CloseAll permanently stops this manager and releases all of its processes.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	m.closed = true
	entries := make([]*browserSession, 0, len(m.sessions))
	for _, e := range m.sessions {
		e.halt.Store(true)
		entries = append(entries, e)
	}
	m.mu.Unlock()
	for _, e := range entries {
		select {
		case <-e.gate:
		default:
			e.requestStop()
			<-e.gate
		}
		e.close()
		e.gate <- struct{}{}
	}
}

func (e *browserSession) connect(ctx context.Context, opts Options) error {
	fingerprint, _ := json.Marshal(opts)
	if e.session != nil && e.fingerprint == string(fingerprint) {
		return nil
	}
	e.close()
	lifeCtx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.stopMu.Lock()
	e.stop = cancel
	e.stopMu.Unlock()
	if e.halt.Load() {
		cancel()
	}
	e.stderr = &boundedBuffer{limit: 4096}
	var err error
	e.command, err = newCommand(lifeCtx, opts.Command, launchArgs(opts))
	if err != nil {
		e.close()
		return fmt.Errorf("找不到可执行的浏览器启动命令: %w", err)
	}
	e.command.Dir = opts.OutputDir
	e.command.Env = launchEnv()
	e.command.Stderr = e.stderr
	e.command.WaitDelay = time.Second
	isolateProcess(e.command)
	// Initialize with a lifetime context; only cancellation during handshake
	// transfers from the invocation. Successful invocations keep the session.
	stop := context.AfterFunc(ctx, cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "autocarto-browser", Version: "1.0.0"}, nil)
	rootPath := filepath.ToSlash(opts.OutputDir)
	if !strings.HasPrefix(rootPath, "/") {
		rootPath = "/" + rootPath
	}
	client.AddRoots(&mcp.Root{URI: (&url.URL{Scheme: "file", Path: rootPath}).String(), Name: "browser-outputs"})
	transport := &observedTransport{inner: &mcp.CommandTransport{Command: e.command, TerminateDuration: 300 * time.Millisecond}}
	session, err := client.Connect(lifeCtx, transport, nil)
	e.connection = transport.connection
	stop()
	if err != nil || ctx.Err() != nil {
		if session != nil {
			e.session = session
		}
		diagnostic := e.stderr.String()
		e.close()
		if ctx.Err() != nil {
			return fmt.Errorf("浏览器启动已取消或超时: %w%s", ctx.Err(), diagnosticSuffix(diagnostic))
		}
		// A failed chdir can be reported as "fork/exec <command>: no such file"
		// even when LookPath already found the command. Report the current working
		// directory state without losing the original process/handshake error.
		if info, dirErr := os.Stat(opts.OutputDir); errors.Is(dirErr, os.ErrNotExist) {
			return fmt.Errorf("浏览器工作目录已不存在（%s）: %w%s", opts.OutputDir, err, diagnosticSuffix(diagnostic))
		} else if dirErr == nil && !info.IsDir() {
			return fmt.Errorf("浏览器工作目录不是目录（%s）: %w%s", opts.OutputDir, err, diagnosticSuffix(diagnostic))
		}
		return fmt.Errorf("浏览器服务启动失败: %w%s", err, diagnosticSuffix(diagnostic))
	}
	e.session = session
	e.fingerprint = string(fingerprint)
	e.dead.Store(false)
	go func() {
		_ = session.Wait()
		<-e.gate
		if e.session == session {
			e.close()
		}
		e.gate <- struct{}{}
	}()
	return nil
}

// Must hold gate. First give MCP a chance to close its browser gracefully, then
// kill the process group as a fallback (npx can spawn a separate Node child).
func (e *browserSession) close() {
	if e.session != nil {
		_ = e.session.Close()
	} else if e.connection != nil {
		// Also own the transport during a failed protocol negotiation, when the
		// SDK may return no ClientSession to the caller.
		_ = e.connection.Close()
	}
	killProcessTree(e.command)
	if e.cancel != nil {
		e.cancel()
	}
	e.stopMu.Lock()
	e.stop = nil
	e.stopMu.Unlock()
	e.session, e.connection, e.command, e.cancel = nil, nil, nil, nil
	e.fingerprint = ""
	e.dead.Store(true)
}

type observedTransport struct {
	inner      mcp.Transport
	connection mcp.Connection
}

func (t *observedTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	connection, err := t.inner.Connect(ctx)
	t.connection = connection
	return connection, err
}

func (e *browserSession) requestStop() {
	e.stopMu.Lock()
	defer e.stopMu.Unlock()
	if e.stop != nil {
		e.stop()
	}
}

func diagnosticSuffix(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return "; " + strings.TrimSpace(s)
}
