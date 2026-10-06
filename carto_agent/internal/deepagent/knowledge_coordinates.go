package deepagent

import (
	"context"
	"fmt"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/geocoding"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/cloudwego/eino/components/model"
)

func coordinateResolver(cm model.BaseChatModel, configDir string, provider *config.LLMProviderConfig) knowledge.CoordinateResolver {
	modelName := ""
	if provider != nil {
		modelName = provider.Model
	}
	return func(ctx context.Context, graph map[string]any, source string) error {
		// A missing/unavailable provider key still allows model estimation.
		key, _ := config.LoadTiandituAPIKey(configDir)
		bounded, cancel := context.WithTimeout(ctx, geocoding.ResolutionTimeout)
		defer cancel()
		return (&geocoding.Resolver{Model: cm, Key: key, ModelOptions: geocoding.NonThinkingOptions(modelName)}).Resolve(bounded, graph, source)
	}
}

func (a *knowledgeSubAgent) enrichCoordinates(ctx context.Context, result *knowledge.Result, source string, progress func(knowledge.Progress)) error {
	if a.resolveCoordinates == nil {
		return knowledge.ValidatePlaceCoordinates(result.Graph)
	}
	if progress != nil {
		progress(knowledge.Progress{Stage: "解析地名并查询天地图坐标"})
	}
	// Commit coordinates transactionally. The caller still retains the extracted
	// graph as an editable draft if lookup fails; cancellation never commits.
	graph := cloneKnowledgeGraph(result.Graph)
	err := a.resolveCoordinates(ctx, graph, source)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("自动定位未完成，已保留抽取记录；可重试自动定位：%w", err)
	}
	if err := knowledge.ValidatePlaceCoordinates(graph); err != nil {
		return err
	}
	result.Graph = graph
	knowledge.SyncResolvedCoordinates(result)
	return nil
}
