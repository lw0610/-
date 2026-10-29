package knowledge

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ValidateReviewDraft checks bounded JSON structure only; incomplete references
// and fields remain editable. Compilation checks its concrete data requirements.
func ValidateReviewDraft(graph Graph) error {
	if graph == nil {
		return fmt.Errorf("%w: graph必须为JSON对象", ErrReviewInvalid)
	}
	raw, err := json.Marshal(graph)
	if err != nil || len(raw) > MaxReviewGraphBytes {
		return fmt.Errorf("%w: 图谱过大或不是有效JSON", ErrReviewInvalid)
	}
	var normalized map[string]any
	if err = json.Unmarshal(raw, &normalized); err != nil {
		return fmt.Errorf("%w: graph格式错误", ErrReviewInvalid)
	}
	nodes := 0
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		nodes++
		if depth > 64 || nodes > 400000 {
			return fmt.Errorf("%w: 图谱嵌套或条目过多", ErrReviewInvalid)
		}
		switch v := value.(type) {
		case map[string]any:
			for _, child := range v {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err = visit(normalized, 0); err != nil {
		return err
	}
	for _, collection := range graphCollections {
		value, exists := normalized[collection.key]
		if !exists {
			continue
		}
		rows, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%w: %s必须是数组", ErrReviewInvalid, collection.key)
		}
		for i, row := range rows {
			if _, ok := row.(map[string]any); !ok {
				return fmt.Errorf("%w: %s[%d]必须是对象", ErrReviewInvalid, collection.key, i)
			}
		}
	}
	return nil
}

func reviewEditable(r *Review) bool {
	return r.Status == ReviewAwaiting || r.Status == ReviewCompileFailed
}
func reviewTouch(r *Review) { r.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano) }

func (s *ReviewStore) SaveDraft(sid, rid string, baseRevision int64, graph Graph, resolutions ...[]ReviewIssueResolutionInput) (*Review, error) {
	return s.saveDraft(sid, rid, baseRevision, graph, false, resolutions...)
}

func (s *ReviewStore) saveDraft(sid, rid string, baseRevision int64, graph Graph, automaticCoordinates bool, resolutions ...[]ReviewIssueResolutionInput) (*Review, error) {
	return s.saveDraftContext(context.Background(), sid, rid, baseRevision, graph, automaticCoordinates, resolutions...)
}

func (s *ReviewStore) saveDraftContext(ctx context.Context, sid, rid string, baseRevision int64, graph Graph, automaticCoordinates bool, resolutions ...[]ReviewIssueResolutionInput) (*Review, error) {
	if err := ValidateReviewDraft(graph); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, err
	}
	if r.Revision != baseRevision || !reviewEditable(r) {
		return nil, ErrReviewConflict
	}
	previousGraph := r.Graph
	r.Graph, err = cloneReviewJSON(graph)
	if err != nil {
		return nil, err
	}
	if automaticCoordinates {
		recordAutomaticCoordinates(r, previousGraph)
	}
	r.Revision++
	if len(resolutions) > 1 {
		return nil, fmt.Errorf("%w: 问题处理记录只能提交一组", ErrReviewInvalid)
	}
	if len(resolutions) == 1 {
		if err = applyReviewIssueResolutions(r, resolutions[0]); err != nil {
			return nil, err
		}
	}
	// Any later edit to a resolved subject invalidates that specific decision.
	// Unrelated edits must neither clear issues nor invalidate their provenance.
	r.Resolutions = validReviewIssueResolutions(r, r.Graph)
	r.Status = ReviewAwaiting
	r.Errors = []ReviewError{}
	r.ClaimRequestID = ""
	r.ClaimedRevision = 0
	r.ClaimedAt = ""
	reviewTouch(r)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReviewStore) SetResume(sid, rid string, resume *ReviewResumeDescriptor) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, err
	}
	r.Resume, err = cloneReviewJSON(resume)
	if err != nil {
		return nil, err
	}
	reviewTouch(r)
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	return r, nil
}

// Claim atomically serializes confirmations. The caller prepares and validates the
// same revision before claiming; no concurrent save can pass this CAS.
func (s *ReviewStore) Claim(sid, rid string, revision int64, requestID string) (*Review, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(requestID) == "" || len(requestID) > 200 {
		return nil, false, fmt.Errorf("%w: 缺少确认请求ID", ErrReviewInvalid)
	}
	if r.Revision != revision {
		return nil, false, ErrReviewConflict
	}
	if r.ClaimRequestID == requestID && r.ClaimedRevision == revision && (r.Status == ReviewCompiling || r.Status == ReviewCompiled) {
		return r, false, nil
	}
	if !reviewEditable(r) {
		return nil, false, ErrReviewConflict
	}
	if issues := ValidateReviewPending(r, r.Graph); len(issues) > 0 {
		return r, false, fmt.Errorf("%w: %s", ErrReviewInvalid, issues[0].Message)
	}
	r.Status = ReviewCompiling
	r.ClaimRequestID = requestID
	r.ClaimedRevision = revision
	r.Errors = []ReviewError{}
	reviewTouch(r)
	r.ClaimedAt = r.UpdatedAt
	if err = s.writeLocked(r); err != nil {
		return nil, false, err
	}
	return r, true, nil
}

// RetryCompilation replaces optional drawing selections for an unchanged,
// already-confirmed revision. Omitted selections return to built-in drawing.
func (s *ReviewStore) RetryCompilation(sid, rid string, revision int64, presets map[string]string) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, err
	}
	if r.Status != ReviewCompileFailed || r.Revision != revision || r.ClaimedRevision != revision || r.ClaimedAt == "" {
		return nil, ErrReviewConflict
	}
	if r.CompileArgs == nil {
		r.CompileArgs = map[string]any{}
	}
	for _, key := range []string{"route_style", "symbols"} {
		delete(r.CompileArgs, key)
		if value := presets[key]; value != "" {
			r.CompileArgs[key] = value
		}
	}
	r.Status = ReviewCompiling
	r.Errors = []ReviewError{}
	reviewTouch(r)
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReviewStore) Complete(sid, rid string, revision int64, approvedGraph Graph, dataset map[string]any) (*Review, error) {
	if err := ValidateReviewDraft(approvedGraph); err != nil {
		return nil, err
	}
	if dir, _ := dataset["dataset_dir"].(string); strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("%w: 编译结果未包含dataset_dir，不能发布已确认版本", ErrReviewInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, err
	}
	if r.Status != ReviewCompiling || r.Revision != revision || r.ClaimedRevision != revision {
		return nil, ErrReviewConflict
	}
	if issues := ValidateReviewPending(r, r.Graph); len(issues) > 0 {
		return r, fmt.Errorf("%w: %s", ErrReviewInvalid, issues[0].Message)
	}
	r.ApprovedGraph, err = cloneReviewJSON(approvedGraph)
	if err != nil {
		return nil, err
	}
	r.Dataset, err = cloneReviewJSON(dataset)
	if err != nil {
		return nil, err
	}
	r.ApprovedRevision = revision
	r.Status = ReviewCompiled
	r.Errors = []ReviewError{}
	reviewTouch(r)
	r.ApprovedAt = r.UpdatedAt
	// Both writes are under the same process lock. Publishing the pointer first
	// keeps a failed record write recoverable as compiling. A crash between them
	// is detected as an explicit index/record mismatch, never an older fallback.
	if err = s.writeConfirmedIndexLocked(r); err != nil {
		return nil, err
	}
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReviewStore) Fail(sid, rid string, revision int64, issues []ReviewError) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, err
	}
	if r.Revision != revision || r.Status != ReviewCompiling {
		return nil, ErrReviewConflict
	}
	r.Status = ReviewCompileFailed
	r.Errors = append([]ReviewError{}, issues...)
	reviewTouch(r)
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReviewStore) SetErrors(sid, rid string, revision int64, issues []ReviewError) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, err
	}
	if r.Revision != revision || !reviewEditable(r) {
		return nil, ErrReviewConflict
	}
	r.Errors = append([]ReviewError{}, issues...)
	reviewTouch(r)
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReviewStore) ImportAsset(sid, rid, sourcePath string) (*ReviewAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, err
	}
	if !reviewEditable(r) {
		return nil, ErrReviewConflict
	}
	raw, err := readReviewImageSource(sourcePath)
	if err != nil {
		return nil, err
	}
	asset, err := s.importAssetBytesLocked(r, raw)
	if err != nil {
		return nil, err
	}
	reviewTouch(r)
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	return asset, nil
}

func readReviewImageSource(sourcePath string) ([]byte, error) {
	if !filepath.IsAbs(sourcePath) {
		return nil, fmt.Errorf("%w: 图片必须为绝对路径", ErrReviewInvalid)
	}
	resolved, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return nil, err
	}
	if resolved != filepath.Clean(sourcePath) {
		return nil, fmt.Errorf("%w: 图片路径不能包含符号链接", ErrReviewInvalid)
	}
	return readReviewFile(sourcePath, MaxReviewImageBytes)
}

// importAssetBytesLocked shares the same validation and content-addressed
// storage for standalone images and workbook pictures. The caller commits the
// review record, so a workbook's graph and asset registry are saved together.
func (s *ReviewStore) importAssetBytesLocked(r *Review, raw []byte) (*ReviewAsset, error) {
	if len(raw) == 0 || len(raw) > MaxReviewImageBytes {
		return nil, fmt.Errorf("%w: 图片为空或超过20 MiB", ErrReviewInvalid)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		return nil, fmt.Errorf("%w: 仅支持有效PNG/JPEG/GIF图片，最多4000万像素", ErrReviewInvalid)
	}
	ext, mime := "", ""
	switch format {
	case "png":
		ext, mime = "png", "image/png"
	case "jpeg":
		ext, mime = "jpg", "image/jpeg"
	case "gif":
		ext, mime = "gif", "image/gif"
	default:
		return nil, fmt.Errorf("%w: 不支持的图片类型", ErrReviewInvalid)
	}
	projectKey := r.ProjectPath
	if projectKey == "" {
		projectKey = "session:" + r.SessionID
	}
	dir := filepath.Join(s.root, "project_assets", reviewHash([]byte(projectKey)))
	if err = s.ensurePath(dir, true); err != nil {
		return nil, err
	}
	asset := ReviewAsset{AssetID: reviewHash(raw) + "." + ext, MediaType: mime, Size: int64(len(raw))}
	asset.ImagePath = filepath.Join(dir, asset.AssetID)
	if err = atomicReviewWrite(asset.ImagePath, raw); err != nil {
		return nil, err
	}
	for _, existing := range r.Assets {
		if existing.AssetID == asset.AssetID {
			return &existing, nil
		}
	}
	r.Assets = append(r.Assets, asset)
	return &asset, nil
}

func (s *ReviewStore) ReadAsset(sid, rid, assetID string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validReviewID(assetID) {
		return nil, "", ErrReviewInvalid
	}
	r, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, "", err
	}
	for _, asset := range r.Assets {
		if asset.AssetID != assetID {
			continue
		}
		if err = s.ensurePath(filepath.Dir(asset.ImagePath), false); err != nil {
			return nil, "", err
		}
		raw, err := readReviewFile(asset.ImagePath, MaxReviewImageBytes)
		if err != nil {
			return nil, "", err
		}
		if reviewHash(raw) != strings.TrimSuffix(assetID, filepath.Ext(assetID)) {
			return nil, "", ErrReviewCorrupt
		}
		return raw, asset.MediaType, nil
	}
	return nil, "", ErrReviewNotFound
}

func (s *ReviewStore) signingKeyLocked() ([]byte, error) {
	if err := s.ensurePath(s.root, true); err != nil {
		return nil, err
	}
	path := filepath.Join(s.root, ".signing_key")
	raw, err := readReviewFile(path, 256)
	if os.IsNotExist(err) {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = atomicReviewWrite(path, []byte(hex.EncodeToString(key))); err != nil {
			return nil, err
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, ErrReviewCorrupt
	}
	return key, nil
}

// SignContext reloads the authoritative saved revision. Callers cannot use this
// helper to sign an arbitrary replacement graph or alter the immutable baseline.
func (s *ReviewStore) SignContext(review *Review) (map[string]any, error) {
	if review == nil {
		return nil, ErrReviewInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(review.SessionID, review.ReviewID)
	if err != nil {
		return nil, err
	}
	if r.Revision != review.Revision {
		return nil, ErrReviewConflict
	}
	decisions := validReviewIssueResolutions(r, r.Graph)
	prepared, preparationErrors := PrepareReviewCompilation(r)
	if len(preparationErrors) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrReviewInvalid, preparationErrors[0].Message)
	}
	key, err := s.signingKeyLocked()
	if err != nil {
		return nil, err
	}
	records := reviewManualRecords(r, prepared)
	sourceGraphJSON, err := json.Marshal(r.Graph)
	if err != nil {
		return nil, err
	}
	adaptations := reviewRenderingAdaptations(r.Graph, prepared)
	graphJSON, err := json.Marshal(prepared)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(struct {
		Version              int                         `json:"version"`
		ReviewID             string                      `json:"review_id"`
		Revision             int64                       `json:"revision"`
		SourceSHA256         string                      `json:"source_sha256"`
		GraphJSON            string                      `json:"graph_json"`
		SourceGraphJSON      string                      `json:"source_graph_json"`
		RenderingAdaptations []ReviewRenderingAdaptation `json:"rendering_adaptations"`
		ManualRecords        []ReviewManualRecord        `json:"manual_records"`
		IssueResolutions     []ReviewIssueResolution     `json:"issue_resolutions,omitempty"`
		InheritedProvenance  []ReviewProvenance          `json:"inherited_provenance,omitempty"`
	}{1, r.ReviewID, r.Revision, r.SourceSHA256, string(graphJSON), string(sourceGraphJSON), adaptations, records, decisions, r.InheritedProvenance})
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return map[string]any{"payload": string(payload), "signature": hex.EncodeToString(mac.Sum(nil))}, nil
}
