package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Credentials remain in Earth Engine's own credential store. This configuration
// contains only runtime preferences, never OAuth tokens or browser cookies.
type IntegrationsConfig struct {
	GEE     GEEConfig     `yaml:"gee" json:"gee"`
	Browser BrowserConfig `yaml:"browser" json:"browser"`
	Web     WebDataConfig `yaml:"web" json:"web"`
}

type GEEConfig struct {
	Enabled        bool   `yaml:"enabled" json:"enabled"`
	Python         string `yaml:"python" json:"python"`
	Project        string `yaml:"project" json:"project"`
	ProxyURL       string `yaml:"proxy_url" json:"proxy_url"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
}

type BrowserConfig struct {
	Enabled        bool     `yaml:"enabled" json:"enabled"`
	Command        string   `yaml:"command" json:"command"`
	Args           []string `yaml:"args" json:"args"`
	Browser        string   `yaml:"browser" json:"browser"`
	Headless       bool     `yaml:"headless" json:"headless"`
	TimeoutSeconds int      `yaml:"timeout_seconds" json:"timeout_seconds"`
}

type WebDataConfig struct {
	Enabled        bool `yaml:"enabled" json:"enabled"`
	TimeoutSeconds int  `yaml:"timeout_seconds" json:"timeout_seconds"`
	MaxDownloadMB  int  `yaml:"max_download_mb" json:"max_download_mb"`
}

func (c *IntegrationsConfig) SetDefaults() {
	if strings.TrimSpace(c.GEE.Python) == "" {
		c.GEE.Python = "python3"
	}
	if c.GEE.TimeoutSeconds == 0 {
		c.GEE.TimeoutSeconds = 300
	}
	if strings.TrimSpace(c.Browser.Command) == "" {
		c.Browser.Command = "npx"
	}
	if c.Browser.Args == nil {
		c.Browser.Args = []string{"-y", "@playwright/mcp@0.0.83"}
	}
	if c.Browser.Browser == "" {
		c.Browser.Browser = "chrome"
	}
	if c.Browser.TimeoutSeconds == 0 {
		c.Browser.TimeoutSeconds = 60
	}
	if c.Web.TimeoutSeconds == 0 {
		c.Web.TimeoutSeconds = 60
	}
	if c.Web.MaxDownloadMB == 0 {
		c.Web.MaxDownloadMB = 100
	}
}

func (c IntegrationsConfig) Validate() error {
	for name, seconds := range map[string]int{"gee": c.GEE.TimeoutSeconds, "browser": c.Browser.TimeoutSeconds, "web": c.Web.TimeoutSeconds} {
		if seconds < 1 || seconds > 3600 {
			return fmt.Errorf("%s timeout_seconds 必须在 1–3600 之间", name)
		}
	}
	if c.Web.MaxDownloadMB < 1 || c.Web.MaxDownloadMB > 1024 {
		return fmt.Errorf("max_download_mb 必须在 1–1024 之间")
	}
	for _, command := range []string{c.GEE.Python, c.Browser.Command} {
		if strings.ContainsAny(command, "\x00\r\n") || len(command) > 4096 {
			return fmt.Errorf("解释器或浏览器命令路径无效")
		}
	}
	if strings.ContainsAny(c.GEE.Project, "\x00\r\n") || len(c.GEE.Project) > 256 {
		return fmt.Errorf("Google Cloud 项目 ID 无效")
	}
	if c.GEE.ProxyURL != "" {
		u, err := url.Parse(c.GEE.ProxyURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("GEE 代理必须是无密码的 HTTP(S) 地址")
		}
	}
	switch c.Browser.Browser {
	case "chrome", "msedge", "chromium":
	default:
		return fmt.Errorf("浏览器必须为 chrome、msedge 或 chromium")
	}
	if len(c.Browser.Args) > 64 {
		return fmt.Errorf("浏览器参数过多")
	}
	for _, arg := range c.Browser.Args {
		if strings.ContainsRune(arg, '\x00') || len(arg) > 4096 {
			return fmt.Errorf("浏览器参数无效")
		}
	}
	return nil
}

func decodeIntegrationsConfig(value any) (IntegrationsConfig, error) {
	var c IntegrationsConfig
	data, err := json.Marshal(value)
	if err != nil {
		return c, err
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return c, fmt.Errorf("integrations 必须是配置对象")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("integrations 配置无效: %w", err)
	}
	c.SetDefaults()
	return c, c.Validate()
}

func (cs *ConfigService) GetIntegrationsConfig() (IntegrationsConfig, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.config == nil {
		return IntegrationsConfig{}, fmt.Errorf("config not loaded")
	}
	c := cs.config.Integrations
	if c.Browser.Args != nil {
		c.Browser.Args = append([]string{}, c.Browser.Args...)
	}
	c.SetDefaults()
	return c, c.Validate()
}
