package flow

import (
	"context"
	"strings"
)

// enrichMode 区分只用确定性规则的基础计划与合并了已缓存增强结果的执行计划；两者都不调用模型。
type enrichMode int

const (
	enrichNone   enrichMode = iota // 基础计划：不读增强缓存
	enrichCached                   // 内部兼容编译：基础计划 + 当前内容键已缓存的增强结果
	enrichSaved                    // 保存/执行预览：只兼容历史成功结果，不再展示增强任务状态
)

// deterministicInputType 依次采用显式类型属性、是/否、可选值、说明中的关键词；
// 都不满足时返回文本，并以 implicit=true 标明这一类型允许由模型细化。
func deterministicInputType(item InputDoc) (typeName string, implicit bool) {
	switch {
	case item.Type != "":
		return item.Type, false
	case item.Boolean:
		return "boolean", false
	case len(item.Options) > 0:
		return "choice", false
	case strings.Contains(item.Description, "对话") && strings.Contains(item.Description, "原文"):
		return "conversation_text", false
	case strings.Contains(item.Description, "文件夹"):
		return "folder", false
	case strings.Contains(item.Description, "文件"):
		return "file", false
	}
	return "text", true
}

// baseInputs 只依据声明生成输入；输入键等于输入名称，模型增强不会改变它。
func baseInputs(declarations []InputDoc) []InputPlan {
	inputs := make([]InputPlan, 0, len(declarations))
	for _, item := range declarations {
		typeName, _ := deterministicInputType(item)
		var defaultValue any
		if item.Default != "" {
			defaultValue = item.Default
			if typeName == "boolean" {
				switch item.Default {
				case "是", "true":
					defaultValue = true
				case "否", "false":
					defaultValue = false
				}
			}
		}
		inputs = append(inputs, InputPlan{
			Key: item.Label, Label: item.Label, Type: typeName, Required: item.Required,
			Default: defaultValue, Options: append([]string{}, item.Options...), Description: item.Description,
			Remember: (typeName == "file" || typeName == "folder") && !item.NoRemember,
		})
	}
	return inputs
}

// DeclaredInputs exposes the shared deterministic declaration rules to local
// input-memory editing, which must not guess types independently of the plan.
func DeclaredInputs(declarations []InputDoc) []InputPlan { return baseInputs(declarations) }

// BasePlan 只做确定性工作：解析、输入、角色绑定、条件、校验与资源链接；永不调用模型。
func (c *Compiler) BasePlan(ctx context.Context, id, scheme string) (*FlowPlan, error) {
	source, err := c.source(ctx, id, scheme)
	if err != nil {
		return nil, err
	}
	plan, err := c.buildPlan(ctx, source, enrichNone)
	if err != nil {
		return nil, err
	}
	return c.linkSource(ctx, source, plan)
}

// Plan 返回保存后可立即预览的执行计划，只兼容当前内容键的历史成功结果；
// 永不调用模型，也不写增强缓存或暴露已移除的增强任务状态。
func (c *Compiler) Plan(ctx context.Context, id, scheme string) (*FlowPlan, error) {
	source, err := c.source(ctx, id, scheme)
	if err != nil {
		return nil, err
	}
	plan, err := c.buildPlan(ctx, source, enrichSaved)
	if err != nil {
		return nil, err
	}
	plan, err = c.linkSource(ctx, source, plan)
	return plan, err
}

// PlanReadOnly returns the saved flow with the same cached input enrichment used
// by execution, without creating a lock or writing any cache.
func (c *Compiler) PlanReadOnly(ctx context.Context, id, scheme string) (*FlowPlan, error) {
	source, err := c.readOnlySource(ctx, id, scheme)
	if err != nil {
		return nil, err
	}
	plan, err := c.buildPlan(ctx, source, enrichSaved)
	if err != nil {
		return nil, err
	}
	plan, err = c.link(ctx, source, plan, false)
	return plan, err
}

func (c *Compiler) readOnlySource(ctx context.Context, id, scheme string) (*compilationSource, error) {
	if c == nil || c.options.Store == nil || c.options.LoadEnvironment == nil {
		return nil, errNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	env, err := c.options.LoadEnvironment(ctx, scheme)
	if err != nil {
		return nil, err
	}
	stored, doc, lock, resources, err := c.options.Store.ResolveResourcesReadOnly(ctx, id, c.options.Resources)
	if err != nil {
		return nil, err
	}
	return c.newSource(ctx, env, stored, doc, lock, resources)
}
