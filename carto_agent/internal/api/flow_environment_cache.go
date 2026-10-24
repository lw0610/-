package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
)

type cachedFlowEnvironment struct {
	revision    string
	environment flow.Environment
}
type flowEnvironmentCache struct {
	mu                   sync.Mutex
	entries              map[string]cachedFlowEnvironment
	metadataRevision     string
	metadataChecked      time.Time
	now                  func() time.Time
	loads, metadataScans uint64
}

// InvalidateFlowEnvironment is called only after a successful asset mutation.
// It retains the Compiler instance and its independently keyed structure cache.
func (a *API) InvalidateFlowEnvironment() {
	cache := &a.flowEnvironments
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.entries = nil
	cache.metadataChecked = time.Time{}
	cache.metadataRevision = ""
}
func (a *API) loadFlowEnvironment(ctx context.Context, scheme string) (flow.Environment, error) {
	if err := ctx.Err(); err != nil {
		return flow.Environment{}, err
	}
	if a.assetCatalogService == nil {
		return flow.Environment{}, fmt.Errorf("方案资产服务尚未配置")
	}
	scheme = strings.TrimSpace(scheme)
	cache := &a.flowEnvironments
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return flow.Environment{}, err
	}
	now := time.Now()
	if cache.now != nil {
		now = cache.now()
	}
	if cache.metadataChecked.IsZero() || now.Sub(cache.metadataChecked) >= time.Second {
		revision, err := flowEnvironmentMetadataRevision(ctx)
		if err != nil {
			return flow.Environment{}, err
		}
		cache.metadataScans++
		cache.metadataRevision = revision
		cache.metadataChecked = now
	}
	profileScheme := scheme
	if profileScheme == "" {
		if cached, ok := cache.entries[scheme]; ok {
			profileScheme = cached.environment.Scheme
		}
	}
	profileRevision, err := flowProfileMetadataRevision(profileScheme)
	if err != nil {
		return flow.Environment{}, err
	}
	revision := cache.metadataRevision + "/" + profileRevision
	if a.configService != nil {
		revision += "/" + a.configService.GetConfigRevision()
	}
	if cached, ok := cache.entries[scheme]; ok && cached.revision == revision {
		return cloneFlowEnvironment(cached.environment), nil
	}
	cache.loads++
	environment, err := a.loadFlowEnvironmentUncached(ctx, scheme)
	if err != nil {
		return environment, err
	}
	if cache.entries == nil {
		cache.entries = map[string]cachedFlowEnvironment{}
	}
	if scheme == "" && profileScheme == "" {
		profileRevision, err = flowProfileMetadataRevision(environment.Scheme)
		if err != nil {
			return flow.Environment{}, err
		}
		revision = cache.metadataRevision + "/" + profileRevision
		if a.configService != nil {
			revision += "/" + a.configService.GetConfigRevision()
		}
	}
	cached := cachedFlowEnvironment{revision: revision, environment: cloneFlowEnvironment(environment)}
	cache.entries[scheme] = cached
	cache.entries[environment.Scheme] = cached
	return cloneFlowEnvironment(environment), nil
}

func flowProfileMetadataRevision(scheme string) (string, error) {
	configDir, err := config.ResolveAutoCartoConfigDir()
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(scheme, "/\\") || scheme == ".." || scheme == "." {
		return "", fmt.Errorf("方案名称无效")
	}
	paths := []string{filepath.Join(configDir, "agent_configs", ".active_agent_config")}
	if scheme != "" {
		paths = append(paths, filepath.Join(configDir, "agent_configs", scheme, "agent.yaml"))
	}
	var revision strings.Builder
	for _, path := range paths {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			fmt.Fprintf(&revision, "missing:%s;", path)
			continue
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&revision, "%s:%d:%d;", path, info.Size(), info.ModTime().UnixNano())
	}
	return revision.String(), nil
}

// This revision reads metadata only. YAML parsing belongs to the cache miss path.
func flowEnvironmentMetadataRevision(ctx context.Context) (string, error) {
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return "", err
	}
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return "", err
	}
	configDir, err := config.ResolveAutoCartoConfigDir()
	if err != nil {
		return "", err
	}
	roots := []string{filepath.Join(assets, "tools"), filepath.Join(assets, "skills"), filepath.Join(home, "tools"), filepath.Join(home, "skills"), filepath.Join(configDir, "tools"), filepath.Join(configDir, "skills"), filepath.Join(configDir, "agent_configs"), filepath.Join(configDir, "agent_configs", ".active_agent_config"), filepath.Join(configDir, "config.yaml")}
	sort.Strings(roots)
	hash := sha256.New()
	for _, root := range roots {
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if os.IsNotExist(walkErr) {
				fmt.Fprintf(hash, "missing:%s\n", path)
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "__pycache__") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "%s\x00%d\x00%d\x00%s\n", path, info.Size(), info.ModTime().UnixNano(), info.Mode())
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func cloneFlowEnvironment(environment flow.Environment) flow.Environment {
	result := environment
	if environment.EnabledFlows != nil {
		enabled := append([]string{}, (*environment.EnabledFlows)...)
		result.EnabledFlows = &enabled
	}
	result.Roles = append([]flow.RoleSpec{}, environment.Roles...)
	for i := range result.Roles {
		result.Roles[i].ToolPackages = append([]string{}, environment.Roles[i].ToolPackages...)
		result.Roles[i].Tools = append([]string{}, environment.Roles[i].Tools...)
	}
	result.Packages = append([]flow.PackageSpec{}, environment.Packages...)
	for i := range result.Packages {
		result.Packages[i].Tools = append([]string{}, environment.Packages[i].Tools...)
	}
	result.Capabilities = append([]flow.CapabilitySpec{}, environment.Capabilities...)
	for i := range result.Capabilities {
		result.Capabilities[i].PackageIDs = append([]string{}, environment.Capabilities[i].PackageIDs...)
		result.Capabilities[i].Keywords = append([]string{}, environment.Capabilities[i].Keywords...)
		result.Capabilities[i].Parameters = append([]string{}, environment.Capabilities[i].Parameters...)
	}
	return result
}
