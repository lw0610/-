package config

import (
	"fmt"
	"os"
	"strings"
)

const TiandituSecretName = "tianditu"

// LoadTiandituAPIKey reads server credentials only. They never enter a graph,
// model prompt, cache identity, or browser response.
func LoadTiandituAPIKey(configDir string) (string, error) {
	if key := strings.TrimSpace(os.Getenv("TIANDITU_API_KEY")); key != "" {
		return key, nil
	}
	if strings.TrimSpace(configDir) == "" {
		var err error
		configDir, err = ResolveAutoCartoConfigDir()
		if err != nil {
			return "", fmt.Errorf("未配置天地图服务器端 Key：请设置 TIANDITU_API_KEY，或在本机密钥存储中配置 tianditu")
		}
	}
	manager, err := NewSecretsManager(configDir)
	if err == nil {
		if key, keyErr := manager.GetAPIKey(TiandituSecretName); keyErr == nil && strings.TrimSpace(key) != "" {
			return strings.TrimSpace(key), nil
		}
	}
	return "", fmt.Errorf("未配置天地图服务器端 Key：请设置 TIANDITU_API_KEY，或在本机密钥存储中配置 tianditu")
}
