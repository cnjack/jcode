package utils

import (
	"os"
	"os/exec"
	"runtime"
)

// OpenURL opens the given URL in the user's default browser. It returns the
// error from launching the platform-specific opener (the command is started,
// not waited on).
func OpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default: // linux, freebsd, etc.
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func GetWorkDir() string {
	// try to get the current working directory
	// if failed, return the home directory
	dir, _ := os.Getwd()
	if dir == "" {
		dir = os.Getenv("HOME")
	}
	return dir
}

func GetSystemInfo() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
