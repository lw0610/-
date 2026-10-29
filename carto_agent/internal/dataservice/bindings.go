package dataservice

import (
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"os"
	"path/filepath"
)

// SaveBindings is called only by the trusted message/attachment handler.
func SaveBindings(sessionDir string, items []Binding) error {
	return atomicJSON(filepath.Join(sessionDir, "data", "provided.json"), items)
}
func LoadBindings(sessionDir string) ([]Binding, error) {
	path := filepath.Join(sessionDir, "data", "provided.json")
	if err := bindings.RejectSymlinks(path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []Binding{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Binding
	err = json.Unmarshal(raw, &items)
	return items, err
}
