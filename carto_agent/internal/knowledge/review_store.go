package knowledge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var reviewRootLocks sync.Map

type ReviewStore struct {
	root string
	mu   *sync.Mutex
}

func NewReviewStore(root string) *ReviewStore {
	root = filepath.Clean(root)
	lock, _ := reviewRootLocks.LoadOrStore(root, &sync.Mutex{})
	return &ReviewStore{root: root, mu: lock.(*sync.Mutex)}
}

func reviewHash(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func validReviewID(id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > 160 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// ensurePath rejects symlinks below the caller-configured storage root.
func (s *ReviewStore) ensurePath(path string, createDir bool) error {
	if s == nil || !filepath.IsAbs(s.root) || filepath.Dir(s.root) == s.root {
		return fmt.Errorf("%w: 非法存储根目录", ErrReviewInvalid)
	}
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%w: 非法存储路径", ErrReviewInvalid)
	}
	current := s.root
	parts := []string{}
	if rel != "." {
		parts = strings.Split(rel, string(os.PathSeparator))
	}
	for i := -1; i < len(parts); i++ {
		if i >= 0 {
			current = filepath.Join(current, parts[i])
		}
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if createDir {
				if err := os.MkdirAll(current, 0700); err != nil {
					return err
				}
				continue
			}
			return statErr
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: 存储目录不可为符号链接或普通文件", ErrReviewInvalid)
		}
	}
	return nil
}

func (s *ReviewStore) recordPath(sid, rid string) (string, error) {
	if !validReviewID(sid) || !validReviewID(rid) {
		return "", fmt.Errorf("%w: 非法会话或草稿ID", ErrReviewInvalid)
	}
	return filepath.Join(s.root, "sessions", sid, rid+".json"), nil
}

type reviewEnvelope struct {
	SHA256 string          `json:"sha256"`
	Review json.RawMessage `json:"review"`
}

func readReviewFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit {
		return nil, fmt.Errorf("%w: 文件类型或大小非法", ErrReviewInvalid)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%w: 文件已变化", ErrReviewInvalid)
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, ErrReviewInvalid
	}
	return raw, nil
}

func atomicReviewWrite(path string, raw []byte) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("%w: 目标文件非法", ErrReviewInvalid)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".review-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *ReviewStore) writeLocked(r *Review) error {
	path, err := s.recordPath(r.SessionID, r.ReviewID)
	if err != nil {
		return err
	}
	if err = s.ensurePath(filepath.Dir(path), true); err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(raw) > 128<<20 {
		return fmt.Errorf("%w: 审核记录超过存储大小限制", ErrReviewInvalid)
	}
	envelope, err := json.Marshal(reviewEnvelope{reviewHash(raw), raw})
	if err != nil {
		return err
	}
	if len(envelope) > 128<<20 {
		return fmt.Errorf("%w: 审核记录超过存储大小限制", ErrReviewInvalid)
	}
	return atomicReviewWrite(path, envelope)
}

func (s *ReviewStore) getLocked(sid, rid string) (*Review, error) {
	path, err := s.recordPath(sid, rid)
	if err != nil {
		return nil, err
	}
	if err = s.ensurePath(filepath.Dir(path), false); os.IsNotExist(err) {
		return nil, ErrReviewNotFound
	} else if err != nil {
		return nil, err
	}
	raw, err := readReviewFile(path, 128<<20)
	if os.IsNotExist(err) {
		return nil, ErrReviewNotFound
	}
	if err != nil {
		return nil, err
	}
	var envelope reviewEnvelope
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return nil, ErrReviewCorrupt
	}
	var compact bytes.Buffer
	if json.Compact(&compact, envelope.Review) != nil || reviewHash(compact.Bytes()) != envelope.SHA256 {
		return nil, ErrReviewCorrupt
	}
	var r Review
	if json.Unmarshal(envelope.Review, &r) != nil || r.Version != 1 || r.SessionID != sid || r.ReviewID != rid || r.SourceSHA256 != reviewHash([]byte(r.SourceText)) || r.SourceSHA256 != r.Contract.SourceSHA256 || r.Revision < 1 {
		return nil, ErrReviewCorrupt
	}
	return &r, nil
}

func (s *ReviewStore) Get(sid, rid string) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(sid, rid)
}

func reviewCompatibility(contract Contract) (string, error) {
	var schema any
	if json.Unmarshal(contract.JSONSchema, &schema) != nil {
		return "", fmt.Errorf("%w: 契约schema非法", ErrReviewInvalid)
	}
	raw, err := json.Marshal(struct {
		ContractID   string
		Schema       any
		Instructions string
		Protocol     string
	}{contract.ContractID, schema, contract.Instructions, "unified-semantic-v3-short-evidence"})
	if err != nil {
		return "", err
	}
	return reviewHash(raw), nil
}

func cloneReviewJSON[T any](value T) (T, error) {
	var copy T
	raw, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(raw, &copy)
	}
	return copy, err
}

func reviewProjectPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Clean(path)
}

func (s *ReviewStore) Create(in ReviewCreateInput) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validReviewID(in.SessionID) || strings.TrimSpace(in.SourceText) == "" || in.Contract.SourceSHA256 != reviewHash([]byte(in.SourceText)) || in.OriginalResult == nil {
		return nil, fmt.Errorf("%w: 缺少原文或抽取结果，或原文hash不匹配", ErrReviewInvalid)
	}
	if in.Graph == nil {
		in.Graph = in.OriginalResult.Graph
	}
	if err := ValidateReviewDraft(in.Graph); err != nil {
		return nil, err
	}
	key, err := reviewCompatibility(in.Contract)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r := &Review{Version: 1, ReviewID: uuid.NewString(), SessionID: in.SessionID, ProjectPath: reviewProjectPath(in.ProjectPath), SessionDir: in.SessionDir, InstanceID: in.InstanceID, SourceText: in.SourceText, SourceSHA256: in.Contract.SourceSHA256, CompatibilityKey: key, Contract: in.Contract, OriginalResult: in.OriginalResult, OriginalCacheInfo: in.OriginalCacheInfo, CompileArgs: in.CompileArgs, ResourceScope: in.ResourceScope, TraceFile: in.TraceFile, Graph: in.Graph, Revision: 1, Status: ReviewAwaiting, Errors: []ReviewError{}, Assets: in.Assets, CreatedAt: now, UpdatedAt: now}
	r.InitialGraph, r.ExtractionStatus, r.PendingIssues = in.Graph, in.ExtractionStatus, in.PendingIssues
	if err = validateReviewPendingIssues(r.PendingIssues); err != nil {
		return nil, err
	}
	if in.ReusedFrom != nil {
		if err = s.inheritReviewProvenanceLocked(r, in.ReusedFrom); err != nil {
			return nil, err
		}
	}
	// Detach all nested maps: caller edits must never mutate the immutable baseline.
	r, err = cloneReviewJSON(r)
	if err != nil {
		return nil, err
	}
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReviewStore) List(sid string) ([]*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validReviewID(sid) {
		return nil, ErrReviewInvalid
	}
	dir := filepath.Join(s.root, "sessions", sid)
	if err := s.ensurePath(dir, false); os.IsNotExist(err) {
		return []*Review{}, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := []*Review{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		r, err := s.getLocked(sid, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return reviewTimeAfter(result[i].UpdatedAt, result[j].UpdatedAt) })
	return result, nil
}

// FindConfirmed only considers successful published snapshots, never drafts.
func (s *ReviewStore) FindConfirmed(projectPath, source string, contract Contract) (*Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projectPath = reviewProjectPath(projectPath)
	if projectPath == "" {
		return nil, ErrReviewNotFound
	}
	key, err := reviewCompatibility(contract)
	if err != nil {
		return nil, err
	}
	sourceHash := reviewHash([]byte(source))
	if sourceHash != contract.SourceSHA256 {
		return nil, ErrReviewInvalid
	}
	path := s.confirmedIndexPath(projectPath, sourceHash, key)
	if err = s.ensurePath(filepath.Dir(path), false); os.IsNotExist(err) {
		return nil, ErrReviewNotFound
	} else if err != nil {
		return nil, err
	}
	index, err := s.readConfirmedIndexLocked(path)
	if os.IsNotExist(err) {
		return nil, ErrReviewNotFound
	}
	if err != nil {
		return nil, err
	}
	if index.ProjectHash != reviewHash([]byte(projectPath)) || index.SourceSHA256 != sourceHash || index.CompatibilityKey != key {
		return nil, ErrReviewCorrupt
	}
	r, err := s.getLocked(index.SessionID, index.ReviewID)
	if err != nil {
		return nil, fmt.Errorf("%w: 已确认索引指向的记录不可用: %v", ErrReviewCorrupt, err)
	}
	if r.Status != ReviewCompiled || r.ProjectPath != projectPath || r.SourceSHA256 != sourceHash || r.CompatibilityKey != key || r.ApprovedRevision != index.Revision || r.ApprovedAt != index.ApprovedAt || r.ClaimedAt != index.ClaimedAt || len(r.ApprovedGraph) == 0 {
		return nil, fmt.Errorf("%w: 已确认索引与记录不一致", ErrReviewCorrupt)
	}
	return r, nil
}

func reviewTimeAfter(candidate, previous string) bool {
	candidateTime, candidateErr := time.Parse(time.RFC3339Nano, candidate)
	previousTime, previousErr := time.Parse(time.RFC3339Nano, previous)
	if candidateErr != nil || previousErr != nil {
		return candidate > previous
	}
	return candidateTime.After(previousTime)
}

// One small pointer per project + exact source bytes + compatible contract.
// Confirmation order, not compilation completion order, determines the pointer.
// Model configuration and draft writes cannot change this key.
type confirmedReviewIndex struct {
	Version          int    `json:"version"`
	ProjectHash      string `json:"project_hash"`
	SourceSHA256     string `json:"source_sha256"`
	CompatibilityKey string `json:"compatibility_key"`
	SessionID        string `json:"session_id"`
	ReviewID         string `json:"review_id"`
	Revision         int64  `json:"revision"`
	ClaimedAt        string `json:"claimed_at,omitempty"`
	ApprovedAt       string `json:"approved_at"`
}

func (s *ReviewStore) confirmedIndexPath(project, sourceHash, key string) string {
	return filepath.Join(s.root, "confirmed", reviewHash([]byte(project)), sourceHash+"-"+key+".json")
}

func (s *ReviewStore) writeConfirmedIndexLocked(r *Review) error {
	if r.ProjectPath == "" {
		return nil
	}
	path := s.confirmedIndexPath(r.ProjectPath, r.SourceSHA256, r.CompatibilityKey)
	if err := s.ensurePath(filepath.Dir(path), true); err != nil {
		return err
	}
	if previous, err := s.readConfirmedIndexLocked(path); err == nil {
		if previous.ProjectHash != reviewHash([]byte(r.ProjectPath)) || previous.SourceSHA256 != r.SourceSHA256 || previous.CompatibilityKey != r.CompatibilityKey {
			return ErrReviewCorrupt
		}
		previousClaimedAt := previous.ClaimedAt
		if previousClaimedAt == "" {
			previousClaimedAt = previous.ApprovedAt
		}
		if reviewTimeAfter(previousClaimedAt, r.ClaimedAt) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	index := confirmedReviewIndex{Version: 1, ProjectHash: reviewHash([]byte(r.ProjectPath)), SourceSHA256: r.SourceSHA256, CompatibilityKey: r.CompatibilityKey, SessionID: r.SessionID, ReviewID: r.ReviewID, Revision: r.ApprovedRevision, ClaimedAt: r.ClaimedAt, ApprovedAt: r.ApprovedAt}
	raw, err := json.Marshal(index)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(reviewEnvelope{reviewHash(raw), raw})
	if err != nil {
		return err
	}
	return atomicReviewWrite(path, envelope)
}

func (s *ReviewStore) readConfirmedIndexLocked(path string) (*confirmedReviewIndex, error) {
	raw, err := readReviewFile(path, 64<<10)
	if err != nil {
		return nil, err
	}
	var envelope reviewEnvelope
	var compact bytes.Buffer
	if json.Unmarshal(raw, &envelope) != nil || json.Compact(&compact, envelope.Review) != nil || reviewHash(compact.Bytes()) != envelope.SHA256 {
		return nil, ErrReviewCorrupt
	}
	var index confirmedReviewIndex
	if json.Unmarshal(envelope.Review, &index) != nil || index.Version != 1 || !validReviewID(index.SessionID) || !validReviewID(index.ReviewID) || index.Revision < 1 {
		return nil, fmt.Errorf("%w: 已确认索引格式不完整", ErrReviewCorrupt)
	}
	return &index, nil
}
