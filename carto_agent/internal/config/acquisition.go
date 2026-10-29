package config

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type AcquisitionConfig struct {
	OCREngine        string  `yaml:"ocr_engine" json:"ocr_engine"`
	OCRPath          string  `yaml:"ocr_path" json:"ocr_path"`
	OCRLanguages     string  `yaml:"ocr_languages" json:"ocr_languages"`
	DocumentPython   string  `yaml:"document_python" json:"document_python"`
	NetworkPolicy    string  `yaml:"network_policy" json:"network_policy"`
	FallbackPolicy   string  `yaml:"fallback_policy" json:"fallback_policy"`
	MaxDownloadMB    int     `yaml:"max_download_mb" json:"max_download_mb"`
	StoreMaxGB       float64 `yaml:"store_max_gb" json:"store_max_gb"`
	Concurrency      int     `yaml:"concurrency" json:"concurrency"`
	PerHost          int     `yaml:"per_host" json:"per_host"`
	AutoFallback     *bool   `yaml:"auto_fallback" json:"auto_fallback"`
	WebSearchEnabled bool    `yaml:"web_search_enabled" json:"web_search_enabled"`
	OverpassEndpoint string  `yaml:"overpass_endpoint" json:"overpass_endpoint"`
}

func (c AcquisitionConfig) WithDefaults() AcquisitionConfig {
	if c.OCREngine == "" {
		c.OCREngine = "rapidocr"
	}
	if c.OCRLanguages == "" {
		c.OCRLanguages = "chi_sim+eng"
	}
	if c.DocumentPython == "" {
		c.DocumentPython = "python3"
	}
	if c.NetworkPolicy == "" {
		c.NetworkPolicy = "direct"
	}
	if c.FallbackPolicy == "" {
		c.FallbackPolicy = "ask"
	}
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
	d := c.WithDefaults()
	if d.OCREngine != "tesseract" && d.OCREngine != "rapidocr" {
		return fmt.Errorf("OCR引擎只支持 tesseract 或 rapidocr")
	}
	if d.NetworkPolicy != "direct" && d.NetworkPolicy != "confirm" && d.NetworkPolicy != "deny" {
		return fmt.Errorf("联网获取策略无效")
	}
	if d.FallbackPolicy != "ask" && d.FallbackPolicy != "allow" && d.FallbackPolicy != "deny" {
		return fmt.Errorf("数据获取兜底策略无效")
	}
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

type ReadingStatus struct {
	Mode      string `json:"mode"`
	Model     string `json:"model"`
	Engine    string `json:"engine"`
	Available bool   `json:"available"`
	Message   string `json:"message"`
}

// Local OCR readiness is independent of the model selected for each scheme role.
func (cs *ConfigService) LocalDocumentReadingStatus() ReadingStatus {
	return DocumentReadingStatus(cs.GetAcquisitionConfig(), "", false)
}
func DocumentReadingStatus(cfg AcquisitionConfig, model string, vision bool) ReadingStatus {
	cfg = cfg.WithDefaults()
	result := ReadingStatus{Mode: "unavailable", Model: model, Engine: cfg.OCREngine, Message: "未找到本机 OCR；可配置视觉模型或安装 OCR 依赖"}
	if vision {
		result.Mode = "vision"
		result.Available = true
		result.Message = "当前角色模型支持图片输入"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if cfg.OCREngine == "rapidocr" {
		if exec.CommandContext(ctx, cfg.DocumentPython, "-c", "import rapidocr_onnxruntime").Run() == nil {
			result.Mode = "ocr"
			result.Available = true
			result.Message = "使用本机 RapidOCR"
		}
	} else {
		program := cfg.OCRPath
		if program == "" {
			program = "tesseract"
		}
		raw, err := exec.CommandContext(ctx, program, "--list-langs").Output()
		if err == nil {
			installed := string(raw)
			ready := true
			for _, language := range strings.Split(cfg.OCRLanguages, "+") {
				if !strings.Contains("\n"+installed, "\n"+language+"\n") {
					ready = false
				}
			}
			if ready {
				result.Mode = "ocr"
				result.Available = true
				result.Message = "使用本机 Tesseract OCR"
			} else {
				result.Message = "Tesseract缺少语言模型：" + cfg.OCRLanguages
			}
		}
	}
	return result
}
