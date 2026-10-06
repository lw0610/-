package websocket

import (
	"context"
	"fmt"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/geocoding"
	"github.com/autocarto/carto_agent/internal/knowledge"
)

func (s *DeepServer) GeocodeKnowledgeReview(ctx context.Context, sid, rid string, revision int64) (*knowledge.Review, error) {
	store := s.KnowledgeReviewStore()
	if store == nil {
		return nil, fmt.Errorf("知识核对存储不可用")
	}
	// Validate version before spending model/API requests; SaveDraft checks again.
	review, err := s.GetKnowledgeReview(ctx, sid, rid)
	if err != nil {
		return nil, err
	}
	if review.Revision != revision || (review.Status != knowledge.ReviewAwaiting && review.Status != knowledge.ReviewCompileFailed) {
		return nil, knowledge.ErrReviewConflict
	}
	return store.ResolveDraftCoordinates(ctx, sid, rid, revision, s.ResolveKnowledgeGraphCoordinates)
}

// ResolveKnowledgeGraphCoordinates reuses the normal review geocoder without
// persisting an intermediate review. Workbook import can resolve new points
// before its single revision-checked commit.
func (s *DeepServer) ResolveKnowledgeGraphCoordinates(ctx context.Context, graph map[string]any, source string) error {
	configDir := ""
	if cfg := s.agentRebuildConfigSnapshot(); cfg != nil && cfg.BaseConfig != nil {
		configDir = cfg.BaseConfig.ConfigBaseDir
	}
	// Provider outages or absent credentials fall back to a model estimate.
	key, _ := config.LoadTiandituAPIKey(configDir)
	cm, err := s.resolveRuntimeChatModel("")
	if err != nil {
		return fmt.Errorf("地点名称分析模型不可用，请检查当前模型配置")
	}
	bounded, cancel := context.WithTimeout(ctx, geocoding.ResolutionTimeout)
	defer cancel()
	modelName := ""
	if provider := s.resolveRuntimeProviderConfig(""); provider != nil {
		modelName = provider.Model
	}
	resolver := &geocoding.Resolver{Model: cm, Key: key, ModelOptions: geocoding.NonThinkingOptions(modelName)}
	return resolver.Resolve(bounded, graph, source)
}
