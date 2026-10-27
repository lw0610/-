package control

import (
	"github.com/autocarto/carto_agent/internal/testutil"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testutil.RunIsolatedTests(m)) }
