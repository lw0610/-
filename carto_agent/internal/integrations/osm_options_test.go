package integrations

import (
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestOSMOptionsPreserveConfiguredDownloadLimits(t *testing.T) {
	var settings config.IntegrationsConfig
	settings.SetDefaults()
	settings.OSM.TimeoutSeconds = 90
	settings.OSM.TotalTimeoutSeconds = 1800
	settings.OSM.MaxAttempts = 5
	options := osmOptions(settings, "", "")
	if options.TimeoutSeconds != 90 || options.TotalTimeoutSeconds != 1800 || options.MaxAttempts != 5 {
		t.Fatalf("download limits did not reach OSM client options: %+v", options)
	}
}
