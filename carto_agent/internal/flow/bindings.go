package flow

import (
	"fmt"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource"
)

// NewDefaultStore is also used by the HTTP fallback and acceptance runner. The
// library and every flow share the one binding store created at application boot.
func NewDefaultStore(library *resource.Library) (*Store, error) {
	if library == nil || library.Bindings == nil {
		return nil, fmt.Errorf("资源绑定存储未配置")
	}
	user, err := config.ResolveUserFlowsDir()
	if err != nil {
		return nil, err
	}
	store := NewStore(config.ResolveBuiltinFlowsDir(), user, library.Bindings)
	store.Library = library
	return store, nil
}

func (s *Store) OrphanedBindingCount() (int, error) {
	if s.Bindings == nil {
		return 0, nil
	}
	doc, err := s.Bindings.Snapshot()
	return len(doc.Orphaned), err
}
