package sessionfiles

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

type providedRegistry struct {
	Version     int               `json:"version"`
	Files       map[string]string `json:"files"`
	Directories map[string]string `json:"directories,omitempty"`
}

func (s *Store) providedRegistry() (providedRegistry, error) {
	result := providedRegistry{Version: 2, Files: map[string]string{}, Directories: map[string]string{}}
	path := filepath.Join(s.Dir, ".provided-files.json")
	if err := bindings.RejectSymlinks(path); err != nil {
		return result, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return result, err
	}
	if len(raw) > 4<<20 {
		return result, fmt.Errorf("用户文件授权清单过大")
	}
	var saved providedRegistry
	if err = json.Unmarshal(raw, &saved); err != nil {
		return result, err
	}
	if saved.Version == 0 {
		// Before directory attachments the same file contained an exact-path map.
		if err = json.Unmarshal(raw, &result.Files); err != nil {
			return result, err
		}
	} else if saved.Version == 2 {
		result = saved
	} else {
		return result, fmt.Errorf("未知用户文件授权清单版本：%d", saved.Version)
	}
	if result.Files == nil {
		result.Files = map[string]string{}
	}
	if result.Directories == nil {
		result.Directories = map[string]string{}
	}
	return result, nil
}

func (r providedRegistry) allows(requested, canonical string) bool {
	if r.Files[requested] == canonical {
		info, err := os.Stat(canonical)
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	for _, root := range r.Directories {
		if filepath.IsAbs(root) && within(root, canonical) {
			return true
		}
	}
	return false
}

func (s *Store) providedPathAllowed(requested, canonical string) bool {
	registry, err := s.providedRegistry()
	return err == nil && registry.allows(requested, canonical)
}

// ResolveProvidedPath authorizes only registered external inputs; it does not
// expand a file grant to siblings. Symlink targets must remain in the selected
// directory's original canonical boundary.
func (s *Store) ResolveProvidedPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrOutsideRoot
	}
	requested := filepath.Clean(path)
	canonical, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", err
	}
	registry, err := s.providedRegistry()
	if err != nil {
		return "", err
	}
	if !registry.allows(requested, canonical) {
		return "", ErrOutsideRoot
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return "", fmt.Errorf("只支持普通文件和目录")
	}
	return canonical, nil
}

func (s *Store) registerProvidedPath(path string, directory bool) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrOutsideRoot
	}
	path = filepath.Clean(path)
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return "", fmt.Errorf("附件类型不符：必须明确提供普通文件或目录")
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	registry, err := s.providedRegistry()
	if err != nil {
		return "", err
	}
	paths := registry.Files
	if directory {
		paths = registry.Directories
	}
	if paths[canonical] == canonical && paths[path] == canonical {
		return canonical, nil
	}
	paths[canonical], paths[path] = canonical, canonical
	raw, err := json.Marshal(registry)
	if err != nil {
		return "", err
	}
	if len(raw) > 4<<20 {
		return "", fmt.Errorf("用户文件授权清单过大")
	}
	if err = os.MkdirAll(s.Dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.Dir, ".provided-files-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(s.Dir, ".provided-files.json"))
	}
	return canonical, err
}

// RegisterProvided is a trusted application entry point for a user attachment
// or a validated data-service binding. Do not expose it as an arbitrary file API.
func (s *Store) RegisterProvided(path string, origin Origin, options ...RegistrationOption) (Item, error) {
	canonical, err := s.registerProvidedPath(path, false)
	if err != nil {
		return Item{}, err
	}
	return s.Register(canonical, origin, options...)
}

// RegisterProvidedDirectory authorizes enumeration and reads under exactly the
// directory selected by the user; nothing is copied, parsed or indexed here.
func (s *Store) RegisterProvidedDirectory(path string) error {
	_, err := s.registerProvidedPath(path, true)
	return err
}
