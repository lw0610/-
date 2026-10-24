package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var contentHashes sync.Map
var contentHashReads atomic.Uint64

// ContentHashCount reports actual file content hash reads, excluding metadata checks.
func ContentHashCount() uint64 { return contentHashReads.Load() }

func rejectResourcePath(path string) error { return bindings.RejectSymlinks(path) }
func cachedContentHash(path string, info os.FileInfo) (string, error) {
	key := fmt.Sprintf("%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano())
	if value, ok := contentHashes.Load(key); ok {
		return value.(string), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	contentHashReads.Add(1)
	if _, err = io.Copy(h, file); err != nil {
		return "", err
	}
	value := hex.EncodeToString(h.Sum(nil))
	contentHashes.Store(key, value)
	return value, nil
}
func resourceDigest(item LibraryItem) (string, error) {
	h := sha256.New()
	raw, _ := json.Marshal(item.Manifest)
	fmt.Fprintf(h, "manifest:%s\n", raw)
	paths := []string{}
	info, err := os.Stat(item.Root)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		err = filepath.WalkDir(item.Root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path != item.Root && strings.HasPrefix(entry.Name(), ".") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if err := rejectResourcePath(path); err != nil {
				return err
			}
			if !entry.IsDir() && (item.Type != "data_folder" || DataKindForFile(path, true) != "") {
				paths = append(paths, path)
			}
			return nil
		})
	} else {
		paths = append(paths, item.Root)
		if regularFileExists(item.ManifestPath) {
			paths = append(paths, item.ManifestPath)
		}
	}
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	base := item.Root
	if !info.IsDir() {
		base = filepath.Dir(base)
	}
	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !stat.Mode().IsRegular() {
			return "", fmt.Errorf("资源包含特殊文件")
		}
		rel, _ := filepath.Rel(base, path)
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), stat.Size())
		if item.Type != "data_folder" {
			sum, err := cachedContentHash(path, stat)
			if err != nil {
				return "", err
			}
			fmt.Fprint(h, sum)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
