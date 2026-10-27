package integrations

import (
	"github.com/autocarto/carto_agent/internal/browserruntime"
	"github.com/autocarto/carto_agent/internal/geeruntime"
	"github.com/autocarto/carto_agent/internal/osmdata"
	"github.com/autocarto/carto_agent/internal/webdata"
	"strings"
)

// IntegrationFor shares runtime dispatch ownership; checks themselves stay usable.
func IntegrationFor(toolName string) string {
	if strings.HasSuffix(toolName, "_check_environment") {
		return ""
	}
	switch {
	case browserruntime.Handles(toolName):
		return "browser"
	case geeruntime.Handles(toolName):
		return "gee"
	case osmdata.Handles(toolName):
		return "osm"
	case webdata.Handles(toolName):
		return "web"
	}
	return ""
}
func PackageReady(names []string, enabled map[string]bool) bool {
	nonchecks, available := 0, false
	for _, name := range names {
		if strings.HasSuffix(name, "_check_environment") {
			continue
		}
		nonchecks++
		service := IntegrationFor(name)
		if service == "" {
			return true
		}
		available = available || enabled[service]
	}
	return nonchecks == 0 || available
}
