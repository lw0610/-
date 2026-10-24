package bindings

import (
	"fmt"
	"os"
)

// Consume persists a destination before removing exactly the observed source
// values. This supports crash-safe migration across two independent files:
// retries may see both copies, but never lose the only copy. The callback must
// not call this Store and must itself be idempotent.
func (s *Store) Consume(expected map[string]string, persist func() error) error {
	if len(expected) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, _, err := s.load()
	if err != nil {
		return err
	}
	for key, value := range expected {
		observed, exists := document.Bindings[key]
		if !exists {
			observed, exists = document.Orphaned[key]
		}
		if !exists || observed != value {
			return fmt.Errorf("资源绑定在迁移期间发生变化，请重试")
		}
	}
	original, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if err = s.backup(original); err != nil {
		return err
	}
	if err = persist(); err != nil {
		return err
	}
	for key := range expected {
		delete(document.Bindings, key)
		delete(document.Orphaned, key)
	}
	return s.save(document, nil)
}
