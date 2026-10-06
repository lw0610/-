package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type knowledgeReviewStoreProvider interface{ KnowledgeReviewStore() *knowledge.ReviewStore }
type knowledgeReviewConfirmer interface {
	ConfirmKnowledgeReview(context.Context, string, string, int64, string) (*knowledge.Review, error)
}
type knowledgeReviewGetter interface {
	GetKnowledgeReview(context.Context, string, string) (*knowledge.Review, error)
}

type knowledgeReviewGeocoder interface {
	GeocodeKnowledgeReview(context.Context, string, string, int64) (*knowledge.Review, error)
}

type knowledgeGraphCoordinateResolver interface {
	ResolveKnowledgeGraphCoordinates(context.Context, map[string]any, string) error
}

func (a *API) handleImportKnowledgeReviewCallouts(w http.ResponseWriter, r *http.Request) {
	store := a.knowledgeReviewStore(w, r)
	if store == nil {
		return
	}
	var input struct {
		BaseRevision int64           `json:"base_revision"`
		SourcePath   string          `json:"source_path"`
		Graph        knowledge.Graph `json:"graph,omitempty"`
	}
	if err := decodeKnowledgeReviewRequest(w, r, &input); err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	var resolve knowledge.CoordinateResolver
	if service, ok := a.sessionService.(knowledgeGraphCoordinateResolver); ok {
		resolve = service.ResolveKnowledgeGraphCoordinates
	}
	vars := mux.Vars(r)
	result, err := store.ImportCallouts(r.Context(), vars["id"], vars["review_id"], input.BaseRevision, input.SourcePath, input.Graph, resolve)
	if err != nil {
		latest, _ := store.Get(vars["id"], vars["review_id"])
		writeKnowledgeReviewError(w, r, err, latest)
		return
	}
	Success(w, map[string]any{
		"review": knowledgeReviewView(result.Review), "imported_count": result.ImportedCount,
		"updated_count": result.UpdatedCount, "image_count": result.ImageCount,
		"new_place_count": result.NewPlaceCount, "warnings": result.Warnings,
	})
}

func (a *API) handleGeocodeKnowledgeReview(w http.ResponseWriter, r *http.Request) {
	store := a.knowledgeReviewStore(w, r)
	if store == nil {
		return
	}
	var input struct {
		BaseRevision int64 `json:"base_revision"`
	}
	if err := decodeKnowledgeReviewRequest(w, r, &input); err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	service, ok := a.sessionService.(knowledgeReviewGeocoder)
	if !ok {
		writeKnowledgeReviewError(w, r, fmt.Errorf("地点定位服务不可用"), nil)
		return
	}
	vars := mux.Vars(r)
	review, err := service.GeocodeKnowledgeReview(r.Context(), vars["id"], vars["review_id"], input.BaseRevision)
	if err != nil {
		latest, _ := store.Get(vars["id"], vars["review_id"])
		writeKnowledgeReviewError(w, r, err, latest)
		return
	}
	if review == nil {
		writeKnowledgeReviewError(w, r, fmt.Errorf("地点定位未返回更新后的草稿"), nil)
		return
	}
	Success(w, knowledgeReviewView(review))
}

func (a *API) getKnowledgeReview(ctx context.Context, store *knowledge.ReviewStore, sid, rid string) (*knowledge.Review, error) {
	if getter, ok := a.sessionService.(knowledgeReviewGetter); ok {
		return getter.GetKnowledgeReview(ctx, sid, rid)
	}
	return store.Get(sid, rid)
}

func knowledgeReviewView(record *knowledge.Review) knowledge.ReviewView {
	view := record.View()
	view.Assets = append([]knowledge.ReviewAsset(nil), view.Assets...)
	for index := range view.Assets {
		view.Assets[index].PreviewURL = "/api/sessions/" + url.PathEscape(record.SessionID) + "/knowledge-reviews/" + url.PathEscape(record.ReviewID) + "/assets/" + url.PathEscape(view.Assets[index].AssetID)
	}
	return view
}

func (a *API) knowledgeReviewStore(w http.ResponseWriter, r *http.Request) *knowledge.ReviewStore {
	if !ensureHTTPControlContractVersion(w, r, "knowledge_review") {
		return nil
	}
	if provider, ok := a.sessionService.(knowledgeReviewStoreProvider); ok && provider.KnowledgeReviewStore() != nil {
		return provider.KnowledgeReviewStore()
	}
	writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope("KNOWLEDGE_REVIEW_UNAVAILABLE", "知识审核服务不可用", "knowledge_review", "store", nil), nil)
	return nil
}

func writeKnowledgeReviewError(w http.ResponseWriter, r *http.Request, err error, review *knowledge.Review) {
	status, code := http.StatusInternalServerError, "KNOWLEDGE_REVIEW_FAILED"
	switch {
	case errors.Is(err, knowledge.ErrReviewNotFound):
		status, code = http.StatusNotFound, "KNOWLEDGE_REVIEW_NOT_FOUND"
	case errors.Is(err, knowledge.ErrReviewConflict):
		status, code = http.StatusConflict, "KNOWLEDGE_REVIEW_CONFLICT"
	case errors.Is(err, knowledge.ErrReviewInvalid):
		status, code = http.StatusUnprocessableEntity, "KNOWLEDGE_REVIEW_INVALID"
	case errors.Is(err, knowledge.ErrReviewCorrupt):
		status, code = http.StatusConflict, "KNOWLEDGE_REVIEW_CORRUPT"
	}
	var data any
	if review != nil {
		data = knowledgeReviewView(review)
		if len(review.Errors) > 0 && status == http.StatusInternalServerError {
			status, code = http.StatusUnprocessableEntity, "KNOWLEDGE_REVIEW_INVALID"
		}
	}
	writeControlPlaneError(w, r, status, err, newRequestEnvelope(code, err.Error(), "knowledge_review", "review", "请核对草稿并重试。", nil), data)
}

func decodeKnowledgeReviewRequest(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, knowledge.MaxReviewGraphBytes+(64<<10))
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: 请求JSON格式错误", knowledge.ErrReviewInvalid)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: 请求只能包含一个JSON对象", knowledge.ErrReviewInvalid)
	}
	return nil
}

func (a *API) handleListKnowledgeReviews(w http.ResponseWriter, r *http.Request) {
	store := a.knowledgeReviewStore(w, r)
	if store == nil {
		return
	}
	records, err := store.List(mux.Vars(r)["id"])
	if err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	views := make([]knowledge.ReviewView, 0, len(records))
	for _, record := range records {
		record, err = a.getKnowledgeReview(r.Context(), store, record.SessionID, record.ReviewID)
		if err != nil {
			writeKnowledgeReviewError(w, r, err, nil)
			return
		}
		views = append(views, knowledgeReviewView(record))
	}
	Success(w, views)
}

func (a *API) handleGetKnowledgeReview(w http.ResponseWriter, r *http.Request) {
	store := a.knowledgeReviewStore(w, r)
	if store == nil {
		return
	}
	vars := mux.Vars(r)
	record, err := a.getKnowledgeReview(r.Context(), store, vars["id"], vars["review_id"])
	if err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	Success(w, knowledgeReviewView(record))
}

func (a *API) handleSaveKnowledgeReview(w http.ResponseWriter, r *http.Request) {
	store := a.knowledgeReviewStore(w, r)
	if store == nil {
		return
	}
	var input struct {
		BaseRevision int64                                  `json:"base_revision"`
		Graph        knowledge.Graph                        `json:"graph"`
		Resolutions  []knowledge.ReviewIssueResolutionInput `json:"resolutions,omitempty"`
	}
	if err := decodeKnowledgeReviewRequest(w, r, &input); err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	vars := mux.Vars(r)
	record, err := store.SaveDraft(vars["id"], vars["review_id"], input.BaseRevision, input.Graph, input.Resolutions)
	if err != nil {
		latest, _ := store.Get(vars["id"], vars["review_id"])
		writeKnowledgeReviewError(w, r, err, latest)
		return
	}
	Success(w, knowledgeReviewView(record))
}

func (a *API) handleConfirmKnowledgeReview(w http.ResponseWriter, r *http.Request) {
	store := a.knowledgeReviewStore(w, r)
	if store == nil {
		return
	}
	var input struct {
		BaseRevision int64  `json:"base_revision"`
		RequestID    string `json:"request_id,omitempty"`
	}
	if err := decodeKnowledgeReviewRequest(w, r, &input); err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	confirmer, ok := a.sessionService.(knowledgeReviewConfirmer)
	if !ok {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope("KNOWLEDGE_REVIEW_UNAVAILABLE", "知识审核恢复服务不可用", "knowledge_review", "confirm", nil), nil)
		return
	}
	if input.RequestID == "" {
		input.RequestID = uuid.NewString()
	}
	vars := mux.Vars(r)
	record, err := confirmer.ConfirmKnowledgeReview(r.Context(), vars["id"], vars["review_id"], input.BaseRevision, input.RequestID)
	if err != nil {
		if record == nil {
			record, _ = store.Get(vars["id"], vars["review_id"])
		}
		writeKnowledgeReviewError(w, r, err, record)
		return
	}
	if record == nil {
		writeKnowledgeReviewError(w, r, fmt.Errorf("审核恢复未返回状态"), nil)
		return
	}
	Success(w, knowledgeReviewView(record))
}

func (a *API) handleImportKnowledgeReviewAsset(w http.ResponseWriter, r *http.Request) {
	store := a.knowledgeReviewStore(w, r)
	if store == nil {
		return
	}
	var input struct {
		SourcePath string `json:"source_path"`
	}
	if err := decodeKnowledgeReviewRequest(w, r, &input); err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	vars := mux.Vars(r)
	asset, err := store.ImportAsset(vars["id"], vars["review_id"], input.SourcePath)
	if err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	Success(w, map[string]any{"asset_id": asset.AssetID, "image_path": asset.ImagePath, "media_type": asset.MediaType, "size": asset.Size, "preview_url": "/api/sessions/" + url.PathEscape(vars["id"]) + "/knowledge-reviews/" + url.PathEscape(vars["review_id"]) + "/assets/" + url.PathEscape(asset.AssetID)})
}

func (a *API) handlePreviewKnowledgeReviewAsset(w http.ResponseWriter, r *http.Request) {
	store := a.knowledgeReviewStore(w, r)
	if store == nil {
		return
	}
	vars := mux.Vars(r)
	raw, mime, err := store.ReadAsset(vars["id"], vars["review_id"], vars["asset_id"])
	if err != nil {
		writeKnowledgeReviewError(w, r, err, nil)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
