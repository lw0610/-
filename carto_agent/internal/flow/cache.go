package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"sort"
)

// fingerprint 是执行计划的结构标识（CompileKey）：流程原文、编译器版本、方案中各角色的结构、
// 这些角色工具的名称与显示名，以及资源身份。config.yaml 修订号、启用流程、模型和其余能力目录
// 都不进入；模型与候选能力只影响增强内容键（见 enrich_cache.go）。
func (c *Compiler) fingerprint(ctx context.Context, source *compilationSource) (string, error) {
	type roleShape struct {
		ID, DisplayName, Kind, Executor string
		Enabled                         bool
		Tools, ToolPackages             []string
	}
	type capabilityShape struct{ Name, DisplayName string }
	roles := make([]roleShape, 0, len(source.env.Roles))
	used := map[string]bool{}
	for _, role := range source.env.Roles {
		tools := append([]string{}, role.Tools...)
		packages := append([]string{}, role.ToolPackages...)
		sort.Strings(tools)
		sort.Strings(packages)
		// 角色顺序保留：自动步骤按顺序确定性选角。
		roles = append(roles, roleShape{role.ID, role.DisplayName, role.Kind, role.Executor, role.Enabled, tools, packages})
		for _, tool := range tools {
			used[tool] = true
		}
	}
	capabilities := []capabilityShape{}
	for _, capability := range source.env.Capabilities {
		if used[capability.Name] {
			capabilities = append(capabilities, capabilityShape{capability.Name, capability.DisplayName})
		}
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].Name < capabilities[j].Name })
	digest := sha256.New()
	writeHashJSON(digest, struct {
		Raw, Version string
		Roles        []roleShape
		Capabilities []capabilityShape
	}{source.stored.FlowMD, c.options.Version, roles, capabilities})
	// Resource identity affects model context; data bytes and bindings do not.
	writeHashJSON(digest, source.doc.Resources)
	for _, item := range source.resources {
		writeHashJSON(digest, struct{ ID, Type, Version, Owner string }{item.ID, item.Type, item.Version, item.Owner})
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func writeHashJSON(digest hash.Hash, value any) {
	encoded, _ := json.Marshal(value)
	fmt.Fprintf(digest, "%d:", len(encoded))
	_, _ = digest.Write(encoded)
}

func hashKey(value any) string {
	digest := sha256.New()
	writeHashJSON(digest, value)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func (c *Compiler) Enabled(ctx context.Context, id, scheme string) (bool, error) {
	if c == nil || c.options.LoadEnvironment == nil {
		return false, fmt.Errorf("流程编译服务未配置")
	}
	env, err := c.options.LoadEnvironment(ctx, scheme)
	if err != nil {
		return false, err
	}
	return env.FlowEnabled(id), nil
}
