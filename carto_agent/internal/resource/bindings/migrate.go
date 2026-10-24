package bindings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
)

// LegacyResourceKey is confined to the v1 reader. New declarations never use it.
func LegacyResourceKey(typ, label string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(typ) + "\x00" + strings.TrimSpace(label)))
	return "resource_" + hex.EncodeToString(sum[:6])
}

// PrepareMigration installs a snapshot of declarations without holding any flow
// or library lock. Reads migrate only in memory; Set backs up and writes v2.
func (s *Store) PrepareMigration(candidates map[string][]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.migration = make(map[string][]string, len(candidates))
	for old, keys := range candidates {
		s.migration[old] = append([]string(nil), keys...)
	}
	_, _, err := s.load()
	return err
}

func migrateV1(old Document, candidates map[string][]string) (Document, error) {
	next := Document{APIVersion: APIVersion, Bindings: map[string]string{}, Orphaned: map[string]string{}}
	for key, value := range old.Orphaned {
		next.Orphaned[key] = value
	}
	grouped := map[string]map[string]string{}
	for key, value := range old.Bindings {
		targets := candidates[key]
		target := ""
		if len(targets) == 1 {
			target = targets[0]
		} else if len(targets) == 0 && ValidateKey(key) == nil {
			target = key
		}
		if target == "" || ValidateKey(target) != nil {
			next.Orphaned[key] = value
			continue
		}
		normalized := ""
		if strings.TrimSpace(value) != "" {
			var err error
			normalized, err = config.PreferProjectRelativePath(value)
			if err != nil {
				return next, fmt.Errorf("迁移资源绑定 %s 失败：%w", key, err)
			}
		}
		if grouped[target] == nil {
			grouped[target] = map[string]string{}
		}
		grouped[target][key] = normalized
	}
	for target, sources := range grouped {
		var value string
		first, conflict := true, false
		for _, candidate := range sources {
			if !first && candidate != value {
				conflict = true
			}
			value, first = candidate, false
		}
		if conflict {
			for key := range sources {
				next.Orphaned[key] = old.Bindings[key]
			}
		} else {
			next.Bindings[target] = value
		}
	}
	return next, nil
}
