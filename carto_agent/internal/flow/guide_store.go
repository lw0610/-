package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GuideStore struct {
	Packages    *Store
	ConfigPath  string
	Environment GuideEnvironment
	mu          sync.Mutex
}
type GuideConversionReport struct {
	Converted bool     `json:"converted"`
	Message   string   `json:"message"`
	Items     []string `json:"items"`
}
type StoredGuide struct {
	Meta              FlowMetadata                 `json:"meta"`
	Source            string                       `json:"source"`
	Enabled           bool                         `json:"enabled"`
	GuideMD           string                       `json:"guide_md"`
	Guide             *GuideDoc                    `json:"guide"`
	Resources         []StoredResource             `json:"resources"`
	ResolvedResources []*resource.ResolvedResource `json:"resolved_resources,omitempty"`
	Diagnostics       []Diagnostic                 `json:"diagnostics"`
	ConversionReport  *GuideConversionReport       `json:"conversion_report,omitempty"`
	UpdatedAt         time.Time                    `json:"updated_at"`
	Dir               string                       `json:"-"`
}
type GuideSummary struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Version          string                 `json:"version"`
	Tags             []string               `json:"tags,omitempty"`
	Source           string                 `json:"source"`
	Enabled          bool                   `json:"enabled"`
	UpdatedAt        time.Time              `json:"updated_at"`
	ConversionReport *GuideConversionReport `json:"conversion_report,omitempty"`
}
type ResolvedGuide struct {
	Package    *StoredGuide   `json:"package"`
	Doc        *GuideDoc      `json:"doc"`
	Scope      resource.Scope `json:"scope"`
	LockDigest string         `json:"lock_digest"`
}

func NewGuideStore(packages *Store, configPath string) *GuideStore {
	return &GuideStore{Packages: packages, ConfigPath: configPath}
}
func NewDefaultGuideStore(library *resource.Library) (*GuideStore, error) {
	packages, err := NewDefaultStore(library)
	if err != nil {
		return nil, err
	}
	dir, err := config.ResolveAutoCartoConfigDir()
	if err != nil {
		return nil, err
	}
	store := NewGuideStore(packages, filepath.Join(dir, "guides.yaml"))
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return nil, err
	}
	store.Environment, err = LoadGuideEnvironment(assets)
	if err != nil {
		return nil, err
	}
	return store, nil
}
func documentFilename(raw string) string { return "FLOW.md" }
func packageMainPath(dir string) string {
	p := filepath.Join(dir, "FLOW.md")
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		return p
	}
	return filepath.Join(dir, "GUIDE.md")
}
func (s *GuideStore) decorate(item *StoredFlow) (*StoredGuide, error) {
	env := s.Environment
	if library := s.Packages.ResourceLibrary(); library != nil {
		var err error
		env, err = GuideEnvironmentFromLibrary(library)
		if err != nil {
			return nil, err
		}
	}
	doc := ParseGuide(item.FlowMD, env)
	doc.Meta.ID = item.Meta.ID
	enabled, err := s.enabled(item.Meta.ID)
	if err != nil {
		return nil, err
	}
	g := &StoredGuide{Meta: item.Meta, Source: item.Source, Enabled: enabled, GuideMD: item.FlowMD, Guide: doc, Resources: item.Resources, Diagnostics: doc.Diagnostics, UpdatedAt: item.UpdatedAt, Dir: item.Dir}
	raw, err := readBounded(filepath.Join(item.Dir, "conversion-report.json"), MaxFileBytes)
	if err == nil {
		var report GuideConversionReport
		if err = json.Unmarshal(raw, &report); err != nil {
			return nil, fmt.Errorf("转换报告损坏：%w", err)
		}
		g.ConversionReport = &report
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return g, nil
}
func (s *GuideStore) Get(id string) (*StoredGuide, error) {
	item, err := s.Packages.Get(id)
	if err != nil {
		return nil, err
	}
	return s.decorate(item)
}
func (s *GuideStore) List() ([]GuideSummary, error) {
	items, err := s.Packages.List()
	if err != nil {
		return nil, err
	}
	out := []GuideSummary{}
	for _, item := range items {
		g, err := s.Get(item.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, GuideSummary{ID: g.Meta.ID, Name: g.Meta.Name, Description: g.Meta.Description, Version: g.Meta.Version, Tags: g.Meta.Tags, Source: g.Source, Enabled: g.Enabled, UpdatedAt: g.UpdatedAt, ConversionReport: g.ConversionReport})
	}
	return out, nil
}
func (s *GuideStore) Create(raw, id string) (*StoredGuide, error) {
	item, err := s.Packages.Create(raw, id)
	if err != nil {
		return nil, err
	}
	return s.decorate(item)
}
func (s *GuideStore) Save(id, raw string) (*StoredGuide, error) {
	item, err := s.Packages.Save(id, raw)
	if err != nil {
		return nil, err
	}
	return s.decorate(item)
}
func (s *GuideStore) Copy(id, newID, newName string) (*StoredGuide, error) {
	item, err := s.Packages.Copy(id, newID, newName)
	if err != nil {
		return nil, err
	}
	return s.decorate(item)
}
func (s *GuideStore) Delete(id string) error { return s.Packages.Delete(id) }
func (s *GuideStore) Resolve(ctx context.Context, id string, library *resource.Library) (*ResolvedGuide, error) {
	s.Packages.SetLibrary(library)
	item, _, lock, linked, err := s.Packages.ResolveResourcesReadOnly(ctx, id, library)
	if err != nil {
		return nil, err
	}
	g, err := s.decorate(item)
	if err != nil {
		return nil, err
	}
	env, err := GuideEnvironmentFromLibrary(library)
	if err != nil {
		return nil, err
	}
	env = guideEnvironmentWithLock(env, lock)
	g.Guide = ParseGuide(item.FlowMD, env)
	g.Guide.Meta.ID = id
	g.Diagnostics = append([]Diagnostic{}, g.Guide.Diagnostics...)
	g.ResolvedResources = linked
	for _, r := range linked {
		for _, d := range r.Problems {
			code := "FLOW_RESOURCE_NOT_FOUND"
			if strings.Contains(d.Code, "AMBIGUOUS") {
				code = "FLOW_RESOURCE_AMBIGUOUS"
			}
			exists := false
			for _, prior := range g.Diagnostics {
				if prior.Code == code && strings.Contains(prior.Message, r.Label) {
					exists = true
				}
			}
			if !exists {
				g.Diagnostics = append(g.Diagnostics, diagnostic("warning", code, "", 0, r.Label+"："+d.Message))
			}
		}
	}
	encoded, _ := json.Marshal(lock)
	sum := sha256.Sum256(encoded)
	return &ResolvedGuide{Package: g, Doc: g.Guide, Scope: resource.Scope{FlowID: id, FlowDir: item.Dir, Lock: lock, Linked: linked}, LockDigest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

type guideSettings struct {
	Disabled []string `yaml:"disabled"`
}

func (s *GuideStore) settings() (guideSettings, error) {
	var out guideSettings
	if s.ConfigPath == "" {
		return out, nil
	}
	raw, err := readBounded(s.ConfigPath, 1<<20)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = yaml.Unmarshal(raw, &out)
	return out, err
}
func (s *GuideStore) enabled(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.settings()
	return !containsGuideString(cfg.Disabled, id), err
}
func (s *GuideStore) SetEnabled(id string, enabled bool) (*StoredGuide, error) {
	if _, err := s.Packages.Get(id); err != nil {
		return nil, err
	}
	if s.ConfigPath == "" {
		return nil, fmt.Errorf("指南配置目录未配置")
	}
	err := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		cfg, err := s.settings()
		if err != nil {
			return err
		}
		next := []string{}
		for _, x := range cfg.Disabled {
			if x != id {
				next = append(next, x)
			}
		}
		if !enabled {
			next = append(next, id)
		}
		cfg.Disabled = next
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		return writeGuideFile(s.ConfigPath, raw)
	}()
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}
func writeGuideFile(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := rejectSymlinks(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".guide-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
