package websocket

import (
	"fmt"
	"time"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/session"
)

// 消息类型常量
const (
	MsgTypeStatus              = "status"    // 状态更新
	MsgTypeThinking            = "thinking"  // 思考中
	MsgTypeTool                = "tool"      // 工具调用
	MsgTypeAgent               = "agent"     // 子智能体调用
	MsgTypeResponse            = "response"  // 最终响应
	MsgTypeError               = "error"     // 错误
	MsgTypeCancelled           = "cancelled" // 已取消
	MsgTypeTaskCreate          = "task"
	MsgTypeTaskCancel          = "cancel"
	MsgTypeTaskRetry           = "task_retry"
	MsgTypeTaskAck             = "task_ack"
	MsgTypeFlowEvent           = "flow_event"
	MsgTypeSessionFilesChanged = "session_files_changed"
	MsgTypeFlowCompile         = "flow_compile"
	MsgTypeRunEvent            = "run_event"        // 统一运行事件
	MsgTypeTaskListUpdate      = "task_list_update" // 任务列表更新
	MsgTypeAgentWorkflow       = "agent_workflow"   // 智能体工作流事件

	// 配置管理
	MsgTypeConfigGet      = "config_get"       // 获取配置
	MsgTypeConfigSet      = "config_set"       // 设置配置
	MsgTypeConfigResponse = "config_response"  // 配置响应
	MsgTypeConfigGetAll   = "config_get_all"   // 获取完整配置
	MsgTypeConfigBatchSet = "config_batch_set" // 批量设置配置
	MsgTypeConfigChanged  = "config_changed"   // 配置变更推送

	// 用户管理
	MsgTypeUserList     = "user_list"     // 获取用户列表
	MsgTypeUserCreate   = "user_create"   // 创建用户
	MsgTypeUserSwitch   = "user_switch"   // 切换用户
	MsgTypeUserDelete   = "user_delete"   // 删除用户
	MsgTypeUserResponse = "user_response" // 用户操作响应

	// 智能体配置
	MsgTypeAgentConfigList     = "agent_config_list"     // 获取配置列表
	MsgTypeAgentConfigGet      = "agent_config_get"      // 获取配置详情
	MsgTypeAgentConfigSave     = "agent_config_save"     // 保存配置
	MsgTypeAgentConfigDelete   = "agent_config_delete"   // 删除配置
	MsgTypeAgentConfigSwitch   = "agent_config_switch"   // 切换配置
	MsgTypeAgentConfigResponse = "agent_config_response" // 配置响应

	// 服务管理
	MsgTypeServiceStatus   = "service_status"   // 服务状态
	MsgTypeRuntimeSnapshot = "runtime_snapshot" // 运行时全量快照

	// 实例管理
	MsgTypeInstanceList    = "instance_list"    // 获取实例列表
	MsgTypeInstanceChanged = "instance_changed" // 实例变化推送
	MsgTypeInstanceSelect  = "instance_select"  // 选择活动实例

	// HITL 中断确认
	MsgTypeInterruptRequest  = "interrupt_request"  // Go→Python: 中断请求
	MsgTypeInterruptResponse = "interrupt_response" // Python→Go: 中断响应

	// LLM 配置管理
	MsgTypeLLMConfigSave       = "llm_config_save"     // 保存 LLM 配置
	MsgTypeLLMConfigResponse   = "llm_config_response" // LLM 配置响应
	MsgTypeModelQuickSwitch    = "model_quick_switch"  // 会话内快速切换模型
	MsgTypeModeSwitch          = "execution_mode_switch"
	MsgTypeAgentQuickSwitch    = "agent_quick_switch"
	MsgTypeSessionState        = "session_state"
	MsgTypeSessionRuntimeState = "session_runtime_state"
	MsgTypeSessionAttach       = "session_attach"
	MsgTypeSessionDetach       = "session_detach"
	MsgTypePhaseTransition     = "phase_transition"
	MsgTypeModeState           = "mode_state"
	MsgTypeTaskRunningChange   = "task_running_changed"

	// 会话管理
	MsgTypeSessionSave        = "session.save"         // 保存会话
	MsgTypeSessionLoad        = "session.load"         // 加载会话
	MsgTypeSessionList        = "session.list"         // 获取会话列表
	MsgTypeSessionDelete      = "session.delete"       // 删除会话
	MsgTypeSessionDirInit     = "session.dir_init"     // 初始化会话目录
	MsgTypeSessionDirInfo     = "session.dir_info"     // 获取会话目录信息
	MsgTypeSessionResponse    = "session.response"     // 会话操作响应
	MsgTypeSessionListChanged = "session_list_changed" // 会话列表变更广播
	MsgTypeSessionLogReset    = "session_log_reset"    // Go→插件: 会话日志句柄重建

	// 上下文管理
	MsgTypeContextCompress = "context.compress" // 压缩上下文
	MsgTypeContextStats    = "context.stats"    // 获取上下文统计
	MsgTypeContextResponse = "context.response" // 上下文操作响应

	MsgTypeToolCatalogGet  = "tool_catalog_get" // 获取工具目录（含 tool_groups 展开）
	MsgTypeToolCatalogResp = "tool_catalog_response"

	// Skill 管理
	MsgTypeSkillList          = "skill_list"            // 获取 skill 列表
	MsgTypeSkillGet           = "skill_get"             // 获取 skill 内容
	MsgTypeSkillSave          = "skill_save"            // 保存 skill
	MsgTypeSkillDelete        = "skill_delete"          // 删除 skill
	MsgTypeSkillImport        = "skill_import"          // 导入 skill
	MsgTypeSkillResponse      = "skill_response"        // skill 操作响应
	MsgTypeSkillCatalogChange = "skill_catalog_changed" // skill 目录变更推送
)

// 错误码常量。
const (
	ErrorCodeAgentUnavailable           = "AGENT_UNAVAILABLE"
	ErrorCodeCheckpointStale            = "CHECKPOINT_STALE"
	ErrorCodeInvalidExecutionMode       = "INVALID_EXECUTION_MODE"
	ErrorCodeInvalidCursor              = "INVALID_CURSOR"
	ErrorCodePlanProtocolViolation      = "PLAN_PROTOCOL_VIOLATION"
	ErrorCodeBoundInstanceUnavailable   = "BOUND_INSTANCE_UNAVAILABLE"
	ErrorCodeBoundInstanceDisconnected  = "BOUND_INSTANCE_DISCONNECTED"
	ErrorCodeIdempotencyKeyRequired     = "IDEMPOTENCY_KEY_REQUIRED"
	ErrorCodeSessionOwnerRequired       = "SESSION_OWNER_REQUIRED"
	ErrorCodeSessionIDRequired          = "SESSION_ID_REQUIRED"
	ErrorCodeUnsupportedProtocolVersion = "UNSUPPORTED_PROTOCOL_VERSION"
)

const (
	websocketProtocolVersion  = 1
	skillsWatcherPollInterval = 400 * time.Millisecond
	skillsWatcherDebounce     = 250 * time.Millisecond
)

// Message WebSocket消息结构
type Message struct {
	Type             string                 `json:"type"`
	Content          string                 `json:"content"`
	SessionID        string                 `json:"session_id,omitempty"`
	RequestID        string                 `json:"request_id,omitempty"`
	IdempotencyKey   string                 `json:"idempotency_key,omitempty"`
	Data             map[string]interface{} `json:"data,omitempty"`
	Agent            string                 `json:"agent,omitempty"`              // 智能体名称
	AgentDisplayName string                 `json:"agent_display_name,omitempty"` // 智能体显示名称
	Phase            string                 `json:"phase,omitempty"`              // 阶段：thinking/tool_call/tool_result/decision/response
}

// PendingInterrupt 待确认的中断状态
type PendingInterrupt struct {
	FlowOptions     consumeEventsOptions
	InterruptData   map[string]any
	CheckpointID    string
	Runner          *adk.Runner
	Contexts        []*adk.InterruptCtx
	SessionID       string
	ProjectPath     string
	BoundInstanceID string
	Session         *session.Session
	ModePhase       string
}

// AgentRebuildConfig 保存重建 agent 所需的配置
type AgentRebuildConfig struct {
	ChatModel          model.ToolCallingChatModel
	Tools              []tool.BaseTool
	MaxIteration       int
	BuiltinSkillsDir   string
	UserSkillsDir      string
	PluginSkillsDir    string
	BaseConfig         *config.Config
	RegistryGovernance *control.RegistryGovernance
	CapabilityStore    *capability.SnapshotStore
	CapabilityMode     string
}

func copyMessageData(data map[string]any) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	copied := make(map[string]any, len(data))
	for key, value := range data {
		copied[key] = value
	}
	return copied
}

func withWebSocketProtocolVersion(msg Message) Message {
	updated := msg
	data := copyMessageData(msg.Data)
	data["protocol_version"] = websocketProtocolVersion
	updated.Data = data
	return updated
}

func resolveMessageProtocolVersion(msg Message) (int, error) {
	if msg.Data == nil {
		return websocketProtocolVersion, nil
	}
	raw, exists := msg.Data["protocol_version"]
	if !exists {
		return websocketProtocolVersion, nil
	}
	version, ok := parseStrictUint64(raw)
	if !ok || version != websocketProtocolVersion {
		return 0, fmt.Errorf("unsupported websocket protocol version")
	}
	return int(version), nil
}

func parseStrictUint64(raw any) (uint64, bool) {
	switch value := raw.(type) {
	case uint64:
		return value, true
	case uint32:
		return uint64(value), true
	case uint:
		return uint64(value), true
	case int64:
		if value < 0 {
			return 0, false
		}
		return uint64(value), true
	case int:
		if value < 0 {
			return 0, false
		}
		return uint64(value), true
	case float64:
		if value < 0 || value != float64(uint64(value)) {
			return 0, false
		}
		return uint64(value), true
	case float32:
		if value < 0 || value != float32(uint64(value)) {
			return 0, false
		}
		return uint64(value), true
	case string:
		if value == "" {
			return 0, false
		}
		var parsed uint64
		for _, ch := range value {
			if ch < '0' || ch > '9' {
				return 0, false
			}
			parsed = parsed*10 + uint64(ch-'0')
		}
		return parsed, true
	default:
		return 0, false
	}
}
