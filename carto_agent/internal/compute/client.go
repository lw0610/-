package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/logger"
)

type ToolInvoker interface {
	ListInstanceTools(context.Context, string) ([]map[string]any, error)
	InvokeTool(context.Context, string, map[string]any) (*InvokeResponse, error)
}

type clientTransport interface {
	ListTools(context.Context) ([]ToolDefinition, error)
	Invoke(context.Context, InvokeRequest) (*InvokeResponse, error)
}

type Client struct {
	transport clientTransport
	timeout   time.Duration

	mu           sync.RWMutex
	cachedTools  []ToolDefinition
	pollStopChan chan struct{}
}

type wsTransport struct {
	invoker    ToolInvoker
	instanceID string
}

func NewWSClient(invoker ToolInvoker, instanceID string, timeout time.Duration) *Client {
	return &Client{
		transport: &wsTransport{invoker: invoker, instanceID: instanceID},
		timeout:   timeout,
	}
}

func (c *Client) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	transport, callCtx, cancel, err := c.prepareCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	tools, err := transport.ListTools(callCtx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cachedTools = append([]ToolDefinition(nil), tools...)
	c.mu.Unlock()
	return tools, nil
}

func (c *Client) CachedTools() []ToolDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ToolDefinition, len(c.cachedTools))
	copy(out, c.cachedTools)
	return out
}

func (c *Client) Invoke(ctx context.Context, reqBody InvokeRequest) (*InvokeResponse, error) {
	transport, callCtx, cancel, err := c.prepareCall(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return transport.Invoke(callCtx, reqBody)
}

func (c *Client) StartPolling(interval time.Duration, onChanged func([]ToolDefinition)) {
	c.StopPolling()
	stopCh := make(chan struct{})
	c.mu.Lock()
	c.pollStopChan = stopCh
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var lastSig string
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				tools, err := c.ListTools(context.Background())
				if err != nil {
					logger.Debug("compute tools polling 失败: %v", err)
					continue
				}
				sig := toolSignature(tools)
				if sig != lastSig {
					lastSig = sig
					if onChanged != nil {
						onChanged(tools)
					}
				}
			}
		}
	}()
}

func (c *Client) StopPolling() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pollStopChan != nil {
		close(c.pollStopChan)
		c.pollStopChan = nil
	}
}

func (c *Client) prepareCall(ctx context.Context) (clientTransport, context.Context, context.CancelFunc, error) {
	if c == nil || c.transport == nil {
		return nil, nil, func() {}, fmt.Errorf("compute client transport is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.timeout > 0 {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		return c.transport, callCtx, cancel, nil
	}
	callCtx, cancel := context.WithCancel(ctx)
	return c.transport, callCtx, cancel, nil
}

func (t *wsTransport) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	if t == nil || t.invoker == nil {
		return nil, fmt.Errorf("compute ws transport invoker is nil")
	}
	rawTools, err := t.invoker.ListInstanceTools(ctx, t.instanceID)
	if err != nil {
		return nil, err
	}
	return decodeToolDefinitions(rawTools)
}

func (t *wsTransport) Invoke(ctx context.Context, reqBody InvokeRequest) (*InvokeResponse, error) {
	if t == nil || t.invoker == nil {
		return nil, fmt.Errorf("compute ws transport invoker is nil")
	}
	args := cloneMap(reqBody.Params)
	if args == nil {
		args = map[string]any{}
	}
	if t.instanceID != "" {
		if _, exists := args["instance_id"]; !exists {
			args["instance_id"] = t.instanceID
		}
		if _, exists := args["target_instance_id"]; !exists {
			args["target_instance_id"] = t.instanceID
		}
	}
	result, err := t.invoker.InvokeTool(ctx, reqBody.Tool, args)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("compute ws invoke returned nil result")
	}
	return result, nil
}

func decodeToolDefinitions(rawTools []map[string]any) ([]ToolDefinition, error) {
	if len(rawTools) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(rawTools)
	if err != nil {
		return nil, err
	}
	tools := make([]ToolDefinition, 0, len(rawTools))
	if err := json.Unmarshal(encoded, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func toolSignature(tools []ToolDefinition) string {
	data, _ := json.Marshal(tools)
	return string(data)
}
