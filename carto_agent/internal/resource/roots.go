package resource

import (
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/config"
)

// BuiltinDirectory exposes the library boundary to read-only reference access.
// Consumers must still use Resolver to interpret resources within this root.
func BuiltinDirectory() (string, error) {
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(assets, "resources", "system"), nil
}
