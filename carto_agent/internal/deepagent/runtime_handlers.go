package deepagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	fsmiddleware "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
)

type runtimeHandlerOptions struct {
	AgentName        string
	ActiveConfigName string
	ConfigBaseDir    string
}

func buildRuntimeHandlers(
	ctx context.Context,
	agentName string,
	chatModel model.ToolCallingChatModel,
	providerCfg *config.LLMProviderConfig,
	options runtimeHandlerOptions,
) ([]adk.ChatModelAgentMiddleware, error) {
	handlers := make([]adk.ChatModelAgentMiddleware, 0, 3)

	patchToolCallsHandler, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create patchtoolcalls handler: %w", err)
	}
	handlers = append(handlers, patchToolCallsHandler)

	strategy := config.NormalizeCompressStrategy("")
	if providerCfg != nil {
		strategy = config.NormalizeCompressStrategy(providerCfg.CompressStrategy)
	}

	switch strategy {
	case config.CompressStrategySummary:
		summarizationHandler, err := buildSummarizationHandler(ctx, chatModel, providerCfg)
		if err != nil {
			return nil, err
		}
		if summarizationHandler != nil {
			handlers = append(handlers, summarizationHandler)
		}
	case config.CompressStrategyTruncate, config.CompressStrategySlidingWindow:
		reductionFSHandler, reductionHandler, err := buildReductionHandlers(ctx, agentName, providerCfg, strategy, options)
		if err != nil {
			return nil, err
		}
		if reductionFSHandler != nil {
			handlers = append(handlers, reductionFSHandler)
		}
		if reductionHandler != nil {
			handlers = append(handlers, reductionHandler)
		}
	default:
		return withTaskMaterials(handlers), nil
	}

	return withTaskMaterials(handlers), nil
}

func buildSummarizationHandler(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	providerCfg *config.LLMProviderConfig,
) (adk.ChatModelAgentMiddleware, error) {
	triggerTokens := config.ContextCompressionTargetTokens(providerCfg)
	if triggerTokens <= 0 {
		return nil, nil
	}

	summarizationHandler, err := summarization.New(ctx, &summarization.Config{
		Model:   chatModel,
		Trigger: &summarization.TriggerCondition{ContextTokens: triggerTokens},
	})
	if err != nil {
		return nil, fmt.Errorf("create summarization handler: %w", err)
	}
	return summarizationHandler, nil
}

func buildReductionHandlers(
	ctx context.Context,
	agentName string,
	providerCfg *config.LLMProviderConfig,
	strategy string,
	options runtimeHandlerOptions,
) (adk.ChatModelAgentMiddleware, adk.ChatModelAgentMiddleware, error) {
	triggerTokens := config.ContextCompressionTargetTokens(providerCfg)
	if triggerTokens <= 0 {
		return nil, nil, nil
	}

	reductionCfg := &reduction.Config{
		MaxTokensForClear:         int64(triggerTokens),
		ClearRetentionSuffixLimit: reductionRetentionSuffixLimit(strategy),
	}

	var filesystemHandler adk.ChatModelAgentMiddleware
	backend, rootDir, err := buildReductionFilesystemBackend(agentName, options)
	if err == nil && backend != nil {
		filesystemHandler, err = fsmiddleware.New(ctx, &fsmiddleware.MiddlewareConfig{
			Backend: backend,
			LsToolConfig: &fsmiddleware.ToolConfig{
				Disable: true,
			},
			WriteFileToolConfig: &fsmiddleware.ToolConfig{
				Disable: true,
			},
			EditFileToolConfig: &fsmiddleware.ToolConfig{
				Disable: true,
			},
			GlobToolConfig: &fsmiddleware.ToolConfig{
				Disable: true,
			},
			GrepToolConfig: &fsmiddleware.ToolConfig{
				Disable: true,
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("create reduction filesystem handler: %w", err)
		}
		reductionCfg.Backend = backend
		reductionCfg.RootDir = rootDir
	} else {
		reductionCfg.SkipTruncation = true
	}

	reductionHandler, err := reduction.New(ctx, reductionCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create reduction handler: %w", err)
	}
	return filesystemHandler, reductionHandler, nil
}

func buildReductionFilesystemBackend(agentName string, options runtimeHandlerOptions) (*runtimepersist.ScopedFilesystemBackend, string, error) {
	configBaseDir := strings.TrimSpace(options.ConfigBaseDir)
	if configBaseDir == "" {
		return nil, "", nil
	}

	rootDir := runtimepersist.ResolveReductionOffloadRoot(configBaseDir, options.ActiveConfigName, agentName)
	backend, err := runtimepersist.NewScopedFilesystemBackend(rootDir)
	if err != nil {
		return nil, "", err
	}

	return backend, backend.RootDir(), nil
}

func reductionRetentionSuffixLimit(strategy string) int {
	switch config.NormalizeCompressStrategy(strategy) {
	case config.CompressStrategySlidingWindow:
		return 3
	default:
		return 1
	}
}
