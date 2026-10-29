package flowrun

import (
	"context"
	"github.com/autocarto/carto_agent/internal/flow"
	"os"
	"path/filepath"
	"testing"
)

func TestV7ExistingGeoPackageDeclarationAcceptsGPKGFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "layers.gpkg")
	if err := os.WriteFile(path, []byte("SQLite format 3\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"geopackage", "GeoPackage", "GPKG", ".gpkg"} {
		if err := verifyOutput(context.Background(), flow.OutputDeclaration{Type: "file", Format: format}, path, root, nil); err != nil {
			t.Fatal(format, err)
		}
	}
	if err := verifyOutput(context.Background(), flow.OutputDeclaration{Type: "file", Format: "pdf"}, path, root, nil); err == nil {
		t.Fatal("unrelated format accepted")
	}
}
