package command

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("JCODE_CLOUD_SECRET_BACKEND", "file")
	os.Exit(m.Run())
}
