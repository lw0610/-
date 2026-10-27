package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

const privateCredentialsKey = "__private_credentials"

type tileLoadPermitKey struct{}
type tileLoadPermit struct {
	definitionPath, instanceID string
	used                       atomic.Bool
}

var authConfigID = regexp.MustCompile(`^[A-Za-z0-9]{7}$`)

// AuthorizeConfirmedTileLoad is used only by the deterministic acquisition
// channel. The capability is bound to one definition and the session's QGIS
// instance; no credential is read or placed in public tool arguments here.
func AuthorizeConfirmedTileLoad(ctx context.Context, args map[string]any) (context.Context, error) {
	for key := range args {
		if key != "definition_path" && key != "confirmed" && key != "authcfg" {
			return nil, fmt.Errorf("瓦片加载仅接受系统定义路径和确认参数")
		}
	}
	path, ok := args["definition_path"].(string)
	if !ok || !filepath.IsAbs(path) || strings.TrimSpace(path) != path {
		return nil, fmt.Errorf("瓦片加载需要绝对定义文件路径")
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return nil, fmt.Errorf("瓦片加载需要确认将密钥写入 QGIS 工程")
	}
	if raw, exists := args["authcfg"]; exists {
		id, ok := raw.(string)
		if !ok || id != "" && !authConfigID.MatchString(id) {
			return nil, fmt.Errorf("QGIS 认证配置编号格式无效")
		}
	}
	instance := toolsession.InstanceIDFromContext(ctx)
	if instance == "" {
		return nil, fmt.Errorf("瓦片加载需要会话已绑定的 QGIS 实例")
	}
	return context.WithValue(ctx, tileLoadPermitKey{}, &tileLoadPermit{definitionPath: filepath.Clean(path), instanceID: instance}), nil
}

// authorizedTileArgs never trusts an active-instance fallback or a JSON flag as
// authority. The private context capability cannot be supplied by a model call.
func authorizedTileArgs(ctx context.Context, args map[string]any) (map[string]any, error) {
	permit, _ := ctx.Value(tileLoadPermitKey{}).(*tileLoadPermit)
	if permit == nil {
		return nil, fmt.Errorf("瓦片加载仅允许已确认的数据获取系统调用")
	}
	path, _ := args["definition_path"].(string)
	confirmed, _ := args["confirmed"].(bool)
	if !confirmed || !filepath.IsAbs(path) || filepath.Clean(path) != permit.definitionPath || toolsession.InstanceIDFromContext(ctx) != permit.instanceID {
		return nil, fmt.Errorf("瓦片加载授权与本次请求不匹配")
	}
	for _, key := range []string{"instance_id", "target_instance_id"} {
		if raw, exists := args[key]; exists {
			value, ok := raw.(string)
			if !ok || strings.TrimSpace(value) != permit.instanceID {
				return nil, fmt.Errorf("瓦片加载不能更换已绑定的 QGIS 实例")
			}
		}
	}
	if !permit.used.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("瓦片加载授权已使用")
	}
	copied := cloneToolParams(args)
	copied["target_instance_id"] = permit.instanceID
	return copied, nil
}

func (p *Provider) tileCredential() (string, error) {
	loader := p.loadTileKey
	if loader == nil {
		loader = func() (string, error) { return config.LoadTiandituAPIKey("") }
	}
	key, err := loader()
	if err != nil || strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("天地图密钥不可用，请在设置中配置后重试")
	}
	return strings.TrimSpace(key), nil
}

func redactTileText(value, key string) string {
	if key == "" {
		return value
	}
	for _, variant := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
		value = strings.ReplaceAll(value, variant, "[redacted]")
	}
	return value
}
func redactTileValue(value any, key string) any {
	switch typed := value.(type) {
	case string:
		return redactTileText(typed, key)
	case map[string]any:
		clean := map[string]any{}
		for name, entry := range typed {
			if strings.TrimSpace(name) == privateCredentialsKey {
				continue
			}
			clean[redactTileText(name, key)] = redactTileValue(entry, key)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for i, entry := range typed {
			clean[i] = redactTileValue(entry, key)
		}
		return clean
	default:
		return value
	}
}
func redactTileResult(result *InvokeResult, key string) *InvokeResult {
	raw, err := json.Marshal(result)
	if err != nil {
		return &InvokeResult{Error: "瓦片工具返回无法处理的结果"}
	}
	var value any
	if err = json.Unmarshal(raw, &value); err != nil {
		return &InvokeResult{Error: "瓦片工具返回无法处理的结果"}
	}
	clean, err := json.Marshal(redactTileValue(value, key))
	if err != nil {
		return &InvokeResult{Error: "瓦片工具返回无法处理的结果"}
	}
	var output InvokeResult
	if err = json.Unmarshal(clean, &output); err != nil {
		return &InvokeResult{Error: "瓦片工具返回无法处理的结果"}
	}
	return &output
}
