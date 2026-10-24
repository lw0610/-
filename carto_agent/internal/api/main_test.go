package api

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	os.Exit(m.Run())
}
