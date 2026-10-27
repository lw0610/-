package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strings"
)

type AcquisitionConfig struct {
	MaxDownloadMB    int     `yaml:"max_download_mb" json:"max_download_mb"`
	StoreMaxGB       float64 `yaml:"store_max_gb" json:"store_max_gb"`
	Concurrency      int     `yaml:"concurrency" json:"concurrency"`
	PerHost          int     `yaml:"per_host" json:"per_host"`
	AutoFallback     *bool   `yaml:"auto_fallback" json:"auto_fallback"`
	WebSearchEnabled bool    `yaml:"web_search_enabled" json:"web_search_enabled"`
	OverpassEndpoint string  `yaml:"overpass_endpoint" json:"overpass_endpoint"`
}

func (c AcquisitionConfig) WithDefaults() AcquisitionConfig {
	if c.MaxDownloadMB <= 0 {
		c.MaxDownloadMB = 2048
	}
	if c.StoreMaxGB <= 0 {
		c.StoreMaxGB = 20
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 2
	}
	if c.PerHost <= 0 {
		c.PerHost = 1
	}
	if c.AutoFallback == nil {
		enabled := true
		c.AutoFallback = &enabled
	}
	return c
}
func (c AcquisitionConfig) FallbackEnabled() bool { return c.AutoFallback == nil || *c.AutoFallback }
func (c AcquisitionConfig) Validate() error {
	if strings.TrimSpace(c.OverpassEndpoint) != "" {
		u, err := url.Parse(c.OverpassEndpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("Overpass 地址必须使用不含用户名或密码的 HTTP(S) URL")
		}
	}
	if c.MaxDownloadMB < 1 || c.MaxDownloadMB > 102400 || math.IsNaN(c.StoreMaxGB) || math.IsInf(c.StoreMaxGB, 0) || c.StoreMaxGB <= 0 || c.StoreMaxGB > 10240 || c.Concurrency < 1 || c.Concurrency > 16 || c.PerHost < 1 || c.PerHost > c.Concurrency {
		return fmt.Errorf("数据获取限制无效：下载上限 1–102400 MB，数据仓 0–10240 GB，并发 1–16，单主机并发不超过总并发")
	}
	return nil
}
func (cs *ConfigService) GetAcquisitionConfig() AcquisitionConfig {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config.Acquisition.WithDefaults()
}

// ConfiguredSecretNames never returns values or requests secrets from the OS keychain.
func (cs *ConfigService) ConfiguredSecretNames() []string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	names := []string{}
	if cs.secretsManager != nil {
		if stored, err := cs.secretsManager.ListAPIKeys(); err == nil {
			names = append(names, stored...)
		}
	}
	if _, set := os.LookupEnv("TIANDITU_API_KEY"); set {
		names = append(names, TiandituSecretName)
	}
	sort.Strings(names)
	return names
}
