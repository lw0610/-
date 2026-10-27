package osmdata

import (
	"testing"
	"time"
)

// The accelerated throttle test checks scheduling. This guard also protects the
// production minimum so a default change cannot silently bypass service limits.
func TestDefaultRequestIntervalMeetsPublicServiceMinimum(t *testing.T) {
	if requestInterval < 1100*time.Millisecond {
		t.Fatalf("default OSM request interval %s is below 1.1 seconds", requestInterval)
	}
}
