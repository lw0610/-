package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Secrets 敏感信息结构
type Secrets struct {
	APIKeys     map[string]string `yaml:"api_keys"`
	KeyringKeys []string          `yaml:"keyring_keys,omitempty"`
}

var errSecretNotFound = errors.New("secret not found")

const defaultKeyringServiceName = "autocarto.ai"

type keyringBackend interface {
	Set(name, secret string) error
	Get(name string) (string, error)
	Delete(name string) error
}

// SecretsManager 密钥管理器
type SecretsManager struct {
	configDir   string // 配置目录（通常为 autocarto/config）
	secretsPath string // 密钥文件路径
	masterKey   []byte // 主密钥
	keyring     keyringBackend
}

// NewSecretsManager 创建密钥管理器
// configDir 应该是运行时配置目录路径（通常为 autocarto/config）
func NewSecretsManager(configDir string) (*SecretsManager, error) {
	if disabled, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("AUTOCARTO_DISABLE_KEYRING"))); disabled {
		return newSecretsManagerWithKeyring(configDir, nil)
	}
	return newSecretsManagerWithKeyring(configDir, newOSKeyringBackend(defaultKeyringServiceName))
}

func newSecretsManagerWithKeyring(configDir string, backend keyringBackend) (*SecretsManager, error) {
	if configDir == "" {
		return nil, fmt.Errorf("config directory not specified")
	}

	secretsPath := filepath.Join(configDir, "secrets.enc")

	masterKey, err := loadOrCreateMasterKey(configDir)
	if err != nil {
		return nil, fmt.Errorf("load master key: %w", err)
	}

	return &SecretsManager{
		configDir:   configDir,
		secretsPath: secretsPath,
		masterKey:   masterKey,
		keyring:     backend,
	}, nil
}

// LoadSecrets 加载并解密密钥
func (m *SecretsManager) LoadSecrets() (*Secrets, error) {
	// 如果文件不存在，返回空的 Secrets
	if _, err := os.Stat(m.secretsPath); os.IsNotExist(err) {
		return &Secrets{
			APIKeys:     make(map[string]string),
			KeyringKeys: []string{},
		}, nil
	}

	// 读取加密文件
	encryptedData, err := os.ReadFile(m.secretsPath)
	if err != nil {
		return nil, fmt.Errorf("read secrets file: %w", err)
	}

	// 解密
	decryptedData, err := m.decrypt(encryptedData)
	if err != nil {
		return nil, fmt.Errorf("decrypt secrets: %w", err)
	}

	// 解析 YAML
	secrets := &Secrets{}
	if err := yaml.Unmarshal(decryptedData, secrets); err != nil {
		return nil, fmt.Errorf("parse secrets: %w", err)
	}

	// 确保 map 已初始化
	if secrets.APIKeys == nil {
		secrets.APIKeys = make(map[string]string)
	}
	if secrets.KeyringKeys == nil {
		secrets.KeyringKeys = []string{}
	}

	return secrets, nil
}

// SaveSecrets 加密并保存密钥
func (m *SecretsManager) SaveSecrets(secrets *Secrets) error {
	// 序列化为 YAML
	data, err := yaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}

	// 加密
	encryptedData, err := m.encrypt(data)
	if err != nil {
		return fmt.Errorf("encrypt secrets: %w", err)
	}

	// 确保目录存在
	if err := os.MkdirAll(m.configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// 写入文件（权限 0600）
	if err := os.WriteFile(m.secretsPath, encryptedData, 0600); err != nil {
		return fmt.Errorf("write secrets file: %w", err)
	}

	return nil
}

// SetAPIKey 设置指定配置的 API 密钥
func (m *SecretsManager) SetAPIKey(name, key string) error {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return fmt.Errorf("API key name is empty")
	}
	if m.keyring != nil {
		if err := m.keyring.Set(trimmedName, key); err == nil {
			secrets, loadErr := m.LoadSecrets()
			if loadErr != nil {
				return loadErr
			}
			delete(secrets.APIKeys, trimmedName)
			secrets.KeyringKeys = addKeyringName(secrets.KeyringKeys, trimmedName)
			return m.SaveSecrets(secrets)
		}
	}
	secrets, err := m.LoadSecrets()
	if err != nil {
		return err
	}

	secrets.APIKeys[trimmedName] = key
	secrets.KeyringKeys = removeKeyringName(secrets.KeyringKeys, trimmedName)
	return m.SaveSecrets(secrets)
}

// GetAPIKey 获取指定配置的 API 密钥
func (m *SecretsManager) GetAPIKey(name string) (string, error) {
	trimmedName := strings.TrimSpace(name)
	var keyringErr error
	if m.keyring != nil {
		secret, err := m.keyring.Get(trimmedName)
		if err == nil && strings.TrimSpace(secret) != "" {
			return secret, nil
		}
		keyringErr = err
	}
	secrets, err := m.LoadSecrets()
	if err != nil {
		return "", err
	}

	if key, ok := secrets.APIKeys[trimmedName]; ok {
		return key, nil
	}
	if keyringErr != nil && !errors.Is(keyringErr, errSecretNotFound) {
		return "", keyringErr
	}
	return "", fmt.Errorf("API key not found: %s", trimmedName)
}

// DeleteAPIKey 删除指定配置的 API 密钥
func (m *SecretsManager) DeleteAPIKey(name string) error {
	trimmedName := strings.TrimSpace(name)
	if m.keyring != nil {
		if err := m.keyring.Delete(trimmedName); err != nil && !errors.Is(err, errSecretNotFound) {
			return err
		}
	}
	secrets, err := m.LoadSecrets()
	if err != nil {
		return err
	}

	delete(secrets.APIKeys, trimmedName)
	secrets.KeyringKeys = removeKeyringName(secrets.KeyringKeys, trimmedName)
	return m.SaveSecrets(secrets)
}

// ListAPIKeys 列出所有 API 密钥名称
func (m *SecretsManager) ListAPIKeys() ([]string, error) {
	secrets, err := m.LoadSecrets()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(secrets.APIKeys))
	for name := range secrets.APIKeys {
		names = append(names, name)
	}
	for _, name := range secrets.KeyringKeys {
		if !containsKeyringName(names, name) {
			names = append(names, name)
		}
	}
	return names, nil
}

func addKeyringName(names []string, name string) []string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || containsKeyringName(names, trimmed) {
		return names
	}
	return append(names, trimmed)
}

func removeKeyringName(names []string, name string) []string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return names
	}
	filtered := make([]string, 0, len(names))
	for _, item := range names {
		if strings.TrimSpace(item) == trimmed {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func containsKeyringName(names []string, name string) bool {
	trimmed := strings.TrimSpace(name)
	for _, item := range names {
		if strings.TrimSpace(item) == trimmed {
			return true
		}
	}
	return false
}

type commandKeyringBackend struct {
	service string
}

func newOSKeyringBackend(service string) keyringBackend {
	trimmed := strings.TrimSpace(service)
	if trimmed == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin", "linux":
		return &commandKeyringBackend{service: trimmed}
	default:
		return nil
	}
}

func (b *commandKeyringBackend) Set(name, secret string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("security", "add-generic-password", "-U", "-a", name, "-s", b.service, "-w", secret)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("keyring unavailable: %s", strings.TrimSpace(string(output)))
		}
		return nil
	case "linux":
		cmd := exec.Command("secret-tool", "store", "--label=AutoCartoAI", "service", b.service, "account", name)
		cmd.Stdin = strings.NewReader(secret)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("keyring unavailable: %s", strings.TrimSpace(string(output)))
		}
		return nil
	default:
		return fmt.Errorf("keyring unavailable")
	}
}

func (b *commandKeyringBackend) Get(name string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("security", "find-generic-password", "-a", name, "-s", b.service, "-w")
		output, err := cmd.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(output))
			if strings.Contains(strings.ToLower(text), "could not be found") {
				return "", errSecretNotFound
			}
			return "", fmt.Errorf("keyring unavailable: %s", text)
		}
		return strings.TrimSpace(string(output)), nil
	case "linux":
		cmd := exec.Command("secret-tool", "lookup", "service", b.service, "account", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(output))
			if text == "" {
				return "", errSecretNotFound
			}
			return "", fmt.Errorf("keyring unavailable: %s", text)
		}
		secret := strings.TrimSpace(string(output))
		if secret == "" {
			return "", errSecretNotFound
		}
		return secret, nil
	default:
		return "", fmt.Errorf("keyring unavailable")
	}
}

func (b *commandKeyringBackend) Delete(name string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("security", "delete-generic-password", "-a", name, "-s", b.service)
		output, err := cmd.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(output))
			if strings.Contains(strings.ToLower(text), "could not be found") {
				return errSecretNotFound
			}
			return fmt.Errorf("keyring unavailable: %s", text)
		}
		return nil
	case "linux":
		cmd := exec.Command("secret-tool", "clear", "service", b.service, "account", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(output))
			if text == "" {
				return errSecretNotFound
			}
			return fmt.Errorf("keyring unavailable: %s", text)
		}
		return nil
	default:
		return fmt.Errorf("keyring unavailable")
	}
}

// encrypt 使用 AES-256-GCM 加密数据
func (m *SecretsManager) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(m.masterKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// 生成随机 nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	// 加密并附加 nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Base64 编码
	return []byte(base64.StdEncoding.EncodeToString(ciphertext)), nil
}

// decrypt 使用 AES-256-GCM 解密数据
func (m *SecretsManager) decrypt(ciphertext []byte) ([]byte, error) {
	// Base64 解码
	data, err := base64.StdEncoding.DecodeString(string(ciphertext))
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(m.masterKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertextBytes, nil)
}

// loadOrCreateMasterKey 加载或创建主密钥
func loadOrCreateMasterKey(configDir string) ([]byte, error) {
	keyPath := filepath.Join(configDir, ".master_key")

	// 尝试读取现有密钥
	if data, err := os.ReadFile(keyPath); err == nil {
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}

	// 生成新的 32 字节密钥
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}

	// 确保目录存在
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	// 保存密钥（Base64 编码，权限 0600）
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(keyPath, []byte(encoded), 0600); err != nil {
		return nil, fmt.Errorf("save master key: %w", err)
	}

	return key, nil
}
