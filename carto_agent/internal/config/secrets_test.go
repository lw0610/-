package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretsManagerExplicitDisableUsesOnlyEncryptedLocalStorage(t *testing.T) {
	for _, value := range []string{"1", "true", " TRUE "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("AUTOCARTO_DISABLE_KEYRING", value)
			dir := t.TempDir()
			manager, err := NewSecretsManager(dir)
			if err != nil || manager.keyring != nil {
				t.Fatal("explicit disable still selected an OS keyring backend", err)
			}
			if err := manager.SetAPIKey("isolated-test-provider", "test-only-local-value"); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "secrets.enc"))
			if err != nil || bytes.Contains(raw, []byte("test-only-local-value")) {
				t.Fatal("local fallback is not encrypted", err)
			}
			reloaded, err := NewSecretsManager(dir)
			if err != nil {
				t.Fatal(err)
			}
			if value, err := reloaded.GetAPIKey("isolated-test-provider"); err != nil || value != "test-only-local-value" {
				t.Fatal("encrypted fallback did not round-trip", err)
			}
			if err := reloaded.DeleteAPIKey("isolated-test-provider"); err != nil {
				t.Fatal(err)
			}
			if _, err := reloaded.GetAPIKey("isolated-test-provider"); err == nil {
				t.Fatal("local key deletion failed")
			}
		})
	}
}

type fakeKeyringBackend struct {
	values    map[string]string
	setErr    error
	getErr    error
	deleteErr error
}

func (f *fakeKeyringBackend) Set(name, secret string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.values == nil {
		f.values = make(map[string]string)
	}
	f.values[name] = secret
	return nil
}

func (f *fakeKeyringBackend) Get(name string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	if value, ok := f.values[name]; ok {
		return value, nil
	}
	return "", errSecretNotFound
}

func (f *fakeKeyringBackend) Delete(name string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.values, name)
	return nil
}

func TestSecretsManager_SetAPIKeyPrefersKeyringAndStoresOnlyMetadataInFile(t *testing.T) {
	root := t.TempDir()
	backend := &fakeKeyringBackend{}
	mgr, err := newSecretsManagerWithKeyring(root, backend)
	if err != nil {
		t.Fatalf("newSecretsManagerWithKeyring failed: %v", err)
	}

	if err := mgr.SetAPIKey("chat-main", "sk-keyring"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	if got := backend.values["chat-main"]; got != "sk-keyring" {
		t.Fatalf("expected keyring secret, got %q", got)
	}
	secrets, err := mgr.LoadSecrets()
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}
	if _, ok := secrets.APIKeys["chat-main"]; ok {
		t.Fatalf("expected encrypted file to avoid storing plaintext api key")
	}
	if len(secrets.KeyringKeys) != 1 || secrets.KeyringKeys[0] != "chat-main" {
		t.Fatalf("expected keyring metadata to record provider name, got %#v", secrets.KeyringKeys)
	}
	if _, err := os.Stat(filepath.Join(root, "secrets.enc")); err != nil {
		t.Fatalf("expected encrypted metadata file, stat err: %v", err)
	}
}

func TestSecretsManager_SetAPIKeyFallsBackToEncryptedFileWhenKeyringUnavailable(t *testing.T) {
	root := t.TempDir()
	backend := &fakeKeyringBackend{setErr: errors.New("keyring unavailable")}
	mgr, err := newSecretsManagerWithKeyring(root, backend)
	if err != nil {
		t.Fatalf("newSecretsManagerWithKeyring failed: %v", err)
	}

	if err := mgr.SetAPIKey("chat-main", "sk-file-fallback"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	secrets, err := mgr.LoadSecrets()
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}
	if got := secrets.APIKeys["chat-main"]; got != "sk-file-fallback" {
		t.Fatalf("expected fallback secret in encrypted file, got %q", got)
	}
	if len(secrets.KeyringKeys) != 0 {
		t.Fatalf("expected no keyring metadata after fallback, got %#v", secrets.KeyringKeys)
	}
}

func TestSecretsManager_GetAPIKeyFallsBackToLegacyEncryptedFile(t *testing.T) {
	root := t.TempDir()
	backend := &fakeKeyringBackend{}
	mgr, err := newSecretsManagerWithKeyring(root, backend)
	if err != nil {
		t.Fatalf("newSecretsManagerWithKeyring failed: %v", err)
	}
	if err := mgr.SaveSecrets(&Secrets{APIKeys: map[string]string{"chat-main": "sk-legacy"}}); err != nil {
		t.Fatalf("SaveSecrets failed: %v", err)
	}

	got, err := mgr.GetAPIKey("chat-main")
	if err != nil {
		t.Fatalf("GetAPIKey failed: %v", err)
	}
	if got != "sk-legacy" {
		t.Fatalf("expected legacy file fallback, got %q", got)
	}
}

func TestSecretsManager_DeleteAPIKeyRemovesKeyringValueAndMetadata(t *testing.T) {
	root := t.TempDir()
	backend := &fakeKeyringBackend{values: map[string]string{"chat-main": "sk-keyring"}}
	mgr, err := newSecretsManagerWithKeyring(root, backend)
	if err != nil {
		t.Fatalf("newSecretsManagerWithKeyring failed: %v", err)
	}
	if err := mgr.SaveSecrets(&Secrets{KeyringKeys: []string{"chat-main"}}); err != nil {
		t.Fatalf("SaveSecrets failed: %v", err)
	}

	if err := mgr.DeleteAPIKey("chat-main"); err != nil {
		t.Fatalf("DeleteAPIKey failed: %v", err)
	}
	if _, ok := backend.values["chat-main"]; ok {
		t.Fatalf("expected keyring entry removed")
	}
	secrets, err := mgr.LoadSecrets()
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}
	if len(secrets.KeyringKeys) != 0 {
		t.Fatalf("expected keyring metadata cleared, got %#v", secrets.KeyringKeys)
	}
}
