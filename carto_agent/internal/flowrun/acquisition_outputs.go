package flowrun

import (
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
)

func (c *Controller) applyAcquisitionResult(run *RunState, item flow.AcquisitionItem, result acquire.ItemResult) {
	if len(result.Output.Paths) == 0 {
		return
	}
	path := result.Output.Paths[0]
	if run.AcquiredInputs == nil {
		run.AcquiredInputs = map[string]any{}
	}
	if run.EntryOverrides == nil {
		run.EntryOverrides = map[string]map[string]string{}
	}
	if item.Resource != "" {
		if run.EntryOverrides[item.Resource] == nil {
			run.EntryOverrides[item.Resource] = map[string]string{}
		}
		run.EntryOverrides[item.Resource][item.Entry] = path
		if result.Output.LayerID != "" {
			if run.LayerOverrides == nil {
				run.LayerOverrides = map[string]map[string]string{}
			}
			if run.LayerOverrides[item.Resource] == nil {
				run.LayerOverrides[item.Resource] = map[string]string{}
			}
			run.LayerOverrides[item.Resource][item.Entry] = result.Output.LayerID
		}
		files := map[string]string{}
		switch previous := run.AcquiredInputs[item.Input].(type) {
		case map[string]string:
			for name, path := range previous {
				files[name] = path
			}
		case map[string]any:
			for name, value := range previous {
				if path, ok := value.(string); ok {
					files[name] = path
				}
			}
		}
		files[item.Entry] = path
		if result.Output.LayerID != "" {
			files[item.Entry] = result.Output.LayerID
		}
		run.AcquiredInputs[item.Input] = files
	} else {
		if result.Output.LayerID != "" {
			run.AcquiredInputs[item.Input] = result.Output.LayerID
		} else {
			run.AcquiredInputs[item.Input] = path
		}
	}
	license := ""
	note := item.Entry + "：用户提供"
	if result.SourceID != "" {
		note = item.Entry + "：自动获取；来源：" + result.SourceID
		if run.Acquisition != nil {
			for _, candidate := range append([]flow.SourceChoice{func() flow.SourceChoice {
				if item.Chosen != nil {
					return *item.Chosen
				}
				return flow.SourceChoice{}
			}()}, item.Alternatives...) {
				if candidate.SourceID == result.SourceID {
					license = candidate.License
					note += "；" + candidate.License + "；" + candidate.Attribution
					break
				}
			}
		}
	}
	if len(result.Notes) > 0 {
		note += "；" + strings.Join(result.Notes, "；")
	}
	found := false
	for _, n := range run.SourceNotes {
		found = found || n == note
	}
	if !found {
		run.SourceNotes = append(run.SourceNotes, note)
	}
	run.Plan = flow.ResolveInputResourcesWithLayers(run.Plan, run.Inputs, run.EntryOverrides, run.LayerOverrides)
	run.ResourceFingerprint = run.Plan.LinkFingerprint
	store, err := runSessionFileStore(run)
	if err == nil {
		for _, p := range result.Output.Paths {
			_, _ = store.Register(p, sessionfiles.Origin{Type: "acquisition", Label: item.Entry, SourceID: result.SourceID, License: license}, sessionfiles.WithGroup("data"), sessionfiles.WithFlow(run.ID, ""))
		}
	}
}
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
