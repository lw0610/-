package flowrun

import (
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
)

func (c *Controller) registerFlowOutputs(run *RunState, stepID string, outputs map[string]any) {
	store, err := runSessionFileStore(run)
	if err != nil {
		return
	}
	var visit func(any)
	visit = func(v any) {
		switch value := v.(type) {
		case string:
			path := value
			if !filepath.IsAbs(path) {
				path = filepath.Join(run.SessionDir, path)
			}
			_, _ = store.Register(path, sessionfiles.Origin{Type: "flow", Label: stepID}, sessionfiles.WithFlow(run.ID, stepID))
		case []string:
			for _, path := range value {
				visit(path)
			}
		case []any:
			for _, v := range value {
				visit(v)
			}
		case map[string]any:
			for _, v := range value {
				visit(v)
			}
		}
	}
	visit(outputs)
}

// SessionDir is the trusted location saved by startup. ProjectRoot is an
// application input-resolution root and must never select a session index.
func runSessionFileStore(run *RunState) (*sessionfiles.Store, error) {
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return nil, err
	}
	roots := []string{filepath.Join(home, "data", "acquired")}
	if filepath.IsAbs(run.ProjectPath) {
		roots = append(roots, filepath.Dir(run.ProjectPath))
	}
	return sessionfiles.NewWithRoots(run.SessionID, run.SessionDir, roots...)
}
