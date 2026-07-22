package cloud

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv(secretBackendEnv, "file")
	os.Exit(m.Run())
}
