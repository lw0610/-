package acquire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type CacheSpec struct {
	SourceID       string            `json:"source_id"`
	SourceVersion  string            `json:"source_version"`
	Category       string            `json:"category"`
	Params         map[string]string `json:"params"`
	Query          string            `json:"query"`
	BBox           []float64         `json:"bbox,omitempty"`
	MaskDigest     string            `json:"mask_digest,omitempty"`
	ScaleBand      int               `json:"scale_band"`
	Normalize      any               `json:"normalize"`
	AdapterVersion string            `json:"adapter_version"`
}

func (s CacheSpec) Key() string {
	if len(s.BBox) > 0 {
		s.BBox = append([]float64(nil), s.BBox...)
		for i := range s.BBox {
			s.BBox[i] = math.Round(s.BBox[i]*10000) / 10000
		}
	}
	data, _ := json.Marshal(s)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (s CacheSpec) baseKey() string {
	s.BBox = nil
	s.ScaleBand = 0
	s.Normalize = nil
	return s.Key()
}

type StoredFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type Manifest struct {
	Key         string         `json:"key"`
	Spec        CacheSpec      `json:"spec"`
	CreatedAt   time.Time      `json:"created_at"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	RawFiles    []string       `json:"raw_files"`
	OutputFiles []string       `json:"output_files"`
	Files       []StoredFile   `json:"files"`
	Bytes       int64          `json:"bytes"`
	License     string         `json:"license"`
	Attribution string         `json:"attribution"`
	Notes       []string       `json:"notes"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}
type StoreRecord struct {
	Manifest
	Dir     string `json:"directory"`
	Valid   bool   `json:"valid"`
	Expired bool   `json:"expired"`
}

func (r StoreRecord) Outputs() []string {
	paths := []string{}
	for _, p := range r.OutputFiles {
		paths = append(paths, filepath.Join(r.Dir, filepath.FromSlash(p)))
	}
	return paths
}
func (r StoreRecord) Raw() []string {
	paths := []string{}
	for _, p := range r.RawFiles {
		paths = append(paths, filepath.Join(r.Dir, filepath.FromSlash(p)))
	}
	return paths
}

type Store struct {
	Root     string
	MaxBytes int64
	Now      func() time.Time
}

func NewStore(root string, maxBytes int64) *Store {
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024 * 1024
	}
	return &Store{Root: root, MaxBytes: maxBytes, Now: time.Now}
}

var cacheID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var storeSourceID = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,127}$`)

func (s *Store) lock(ctx context.Context, key string) (func(), error) {
	if err := os.MkdirAll(filepath.Join(s.Root, ".locks"), 0700); err != nil {
		return nil, err
	}
	return lockFile(ctx, filepath.Join(s.Root, ".locks", key+".lock"))
}
func (s *Store) index() ([]StoreRecord, error) {
	records := []StoreRecord{}
	data, err := os.ReadFile(filepath.Join(s.Root, "index.json"))
	if os.IsNotExist(err) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("数据仓索引损坏：%w", err)
	}
	for i := range records {
		if !cacheID.MatchString(records[i].Key) || !storeSourceID.MatchString(records[i].Spec.SourceID) {
			return nil, fmt.Errorf("数据仓索引包含无效路径")
		}
		records[i].Dir = filepath.Join(s.Root, records[i].Spec.SourceID, records[i].Key[:16])
	}
	return records, nil
}
func (s *Store) inspect(record StoreRecord, verify bool) StoreRecord {
	record.Expired = record.ExpiresAt != nil && !s.Now().Before(*record.ExpiresAt)
	record.Valid = true
	for _, file := range record.Files {
		path, err := safeStorePath(record.Dir, file.Path)
		if err != nil {
			record.Valid = false
			break
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != file.Bytes {
			record.Valid = false
			break
		}
		if verify {
			digest, _, err := fileDigest(path)
			if err != nil || digest != file.SHA256 {
				record.Valid = false
				break
			}
		}
	}
	known := map[string]bool{}
	for _, f := range record.Files {
		known[f.Path] = true
	}
	for _, p := range append(append([]string{}, record.OutputFiles...), record.RawFiles...) {
		if _, err := safeStorePath(record.Dir, p); err != nil || !known[p] {
			record.Valid = false
		}
	}
	if len(record.OutputFiles) == 0 {
		record.Valid = false
	}
	return record
}
func (s *Store) Lookup(ctx context.Context, key string) (StoreRecord, bool, error) {
	if !cacheID.MatchString(key) {
		return StoreRecord{}, false, fmt.Errorf("无效缓存键")
	}
	unlock, err := s.lock(ctx, "index")
	if err != nil {
		return StoreRecord{}, false, err
	}
	defer unlock()
	records, err := s.index()
	if err != nil {
		return StoreRecord{}, false, err
	}
	for _, record := range records {
		if record.Key == key {
			record = s.inspect(record, true)
			return record, record.Valid && !record.Expired, nil
		}
	}
	return StoreRecord{}, false, nil
}
func (s *Store) List(ctx context.Context) ([]StoreRecord, error) {
	unlock, err := s.lock(ctx, "index")
	if err != nil {
		return nil, err
	}
	defer unlock()
	records, err := s.index()
	if err != nil {
		return nil, err
	}
	for i := range records {
		records[i] = s.inspect(records[i], false)
	}
	return records, nil
}

// FindContaining reuses raw inputs only; the caller must normalize again for the new extent.
func (s *Store) FindContaining(ctx context.Context, spec CacheSpec) (StoreRecord, bool, error) {
	records, err := s.List(ctx)
	if err != nil {
		return StoreRecord{}, false, err
	}
	sort.Slice(records, func(i, j int) bool { return area(records[i].Spec.BBox) < area(records[j].Spec.BBox) })
	for _, r := range records {
		if r.Valid && !r.Expired && r.Spec.baseKey() == spec.baseKey() && containsBBox(r.Spec.BBox, spec.BBox) && len(r.RawFiles) > 0 {
			verified, ok, err := s.Lookup(ctx, r.Key)
			if err != nil {
				return verified, false, err
			}
			if ok {
				return verified, true, nil
			}
		}
	}
	return StoreRecord{}, false, nil
}

// Materialize holds a process-safe keyed file lock through fetch, verify and commit.
func (s *Store) Materialize(ctx context.Context, spec CacheSpec, refreshDays int, build func(string) (Manifest, error)) (StoreRecord, bool, error) {
	if !storeSourceID.MatchString(spec.SourceID) {
		return StoreRecord{}, false, fmt.Errorf("无效来源编号")
	}
	key := spec.Key()
	unlock, err := s.lock(ctx, key)
	if err != nil {
		return StoreRecord{}, false, err
	}
	defer unlock()
	if record, ok, err := s.Lookup(ctx, key); err != nil || ok {
		return record, ok, err
	}
	sourceRoot := filepath.Join(s.Root, spec.SourceID)
	if err := os.MkdirAll(sourceRoot, 0700); err != nil {
		return StoreRecord{}, false, err
	}
	work, err := os.MkdirTemp(sourceRoot, ".partial-")
	if err != nil {
		return StoreRecord{}, false, err
	}
	defer os.RemoveAll(work)
	for _, dir := range []string{"raw", "out"} {
		if err := os.Mkdir(filepath.Join(work, dir), 0700); err != nil {
			return StoreRecord{}, false, err
		}
	}
	manifest, err := build(work)
	if err != nil {
		return StoreRecord{}, false, err
	}
	if err = ctx.Err(); err != nil {
		return StoreRecord{}, false, err
	}
	manifest.Key = key
	manifest.Spec = spec
	manifest.CreatedAt = s.Now().UTC()
	if refreshDays > 0 {
		expires := manifest.CreatedAt.Add(time.Duration(refreshDays) * 24 * time.Hour)
		manifest.ExpiresAt = &expires
	}
	manifest.Files = nil
	manifest.Bytes = 0
	err = filepath.WalkDir(work, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("数据仓不允许符号链接")
		}
		if entry.IsDir() {
			return nil
		}
		digest, size, err := fileDigest(path)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(work, path)
		manifest.Files = append(manifest.Files, StoredFile{Path: filepath.ToSlash(relative), SHA256: digest, Bytes: size})
		manifest.Bytes += size
		return nil
	})
	if err != nil {
		return StoreRecord{}, false, err
	}
	for _, path := range append(append([]string{}, manifest.RawFiles...), manifest.OutputFiles...) {
		if _, err := safeStorePath(work, path); err != nil {
			return StoreRecord{}, false, err
		}
	}
	if len(manifest.OutputFiles) == 0 {
		return StoreRecord{}, false, fmt.Errorf("获取没有产生可用文件")
	}
	if err = atomicJSON(filepath.Join(work, "manifest.json"), manifest); err != nil {
		return StoreRecord{}, false, err
	}
	unlockIndex, err := s.lock(ctx, "index")
	if err != nil {
		return StoreRecord{}, false, err
	}
	defer unlockIndex()
	records, err := s.index()
	if err != nil {
		return StoreRecord{}, false, err
	}
	kept := []StoreRecord{}
	total := manifest.Bytes
	for _, r := range records {
		if r.Key != key {
			kept = append(kept, r)
			total += r.Bytes
		}
	}
	if total > s.MaxBytes {
		return StoreRecord{}, false, fmt.Errorf("数据仓容量不足，请在设置中清理数据或提高容量上限")
	}
	destination := filepath.Join(sourceRoot, key[:16])
	// The shortened directory is never allowed to overwrite a different full digest.
	if raw, readErr := os.ReadFile(filepath.Join(destination, "manifest.json")); readErr == nil {
		var old Manifest
		if json.Unmarshal(raw, &old) != nil || old.Key != key {
			return StoreRecord{}, false, fmt.Errorf("数据仓目录缓存键冲突")
		}
	}
	backup := work + ".previous"
	hadOld := false
	if _, err := os.Stat(destination); err == nil {
		if err = os.Rename(destination, backup); err != nil {
			return StoreRecord{}, false, err
		}
		hadOld = true
	}
	if err = os.Rename(work, destination); err != nil {
		if hadOld {
			_ = os.Rename(backup, destination)
		}
		return StoreRecord{}, false, err
	}
	record := StoreRecord{Manifest: manifest, Dir: destination, Valid: true}
	kept = append(kept, record)
	sort.Slice(kept, func(i, j int) bool { return kept[i].Key < kept[j].Key })
	if err = atomicJSON(filepath.Join(s.Root, "index.json"), kept); err != nil {
		_ = os.RemoveAll(destination)
		if hadOld {
			_ = os.Rename(backup, destination)
		}
		return StoreRecord{}, false, err
	}
	if hadOld {
		_ = os.RemoveAll(backup)
	}
	return record, false, nil
}
func (s *Store) Delete(ctx context.Context, key string) error {
	if !cacheID.MatchString(key) {
		return fmt.Errorf("无效缓存键")
	}
	unlock, err := s.lock(ctx, key)
	if err != nil {
		return err
	}
	defer unlock()
	unlockIndex, err := s.lock(ctx, "index")
	if err != nil {
		return err
	}
	defer unlockIndex()
	records, err := s.index()
	if err != nil {
		return err
	}
	kept := []StoreRecord{}
	found := false
	for _, r := range records {
		if r.Key == key {
			if err = os.RemoveAll(r.Dir); err != nil {
				return err
			}
			found = true
		} else {
			kept = append(kept, r)
		}
	}
	if !found {
		return os.ErrNotExist
	}
	return atomicJSON(filepath.Join(s.Root, "index.json"), kept)
}
func safeStorePath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) || relative == "" || strings.Contains(relative, `\`) {
		return "", fmt.Errorf("数据仓路径无效")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("数据仓路径越界")
	}
	path := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("数据仓路径越界")
	}
	return resolved, nil
}
func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), count, err
}
func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".json-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func area(b []float64) float64 {
	if len(b) != 4 {
		return math.Inf(1)
	}
	return (b[2] - b[0]) * (b[3] - b[1])
}
func containsBBox(outer, inner []float64) bool {
	return len(outer) == 4 && len(inner) == 4 && outer[0] <= inner[0] && outer[1] <= inner[1] && outer[2] >= inner[2] && outer[3] >= inner[3]
}
