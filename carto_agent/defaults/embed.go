package defaults

import "embed"

// FS 内嵌 APP 端默认配置资产。
//
//go:embed config.yaml all:agent_configs architecture/*.yaml system_experts/*.md
var FS embed.FS
