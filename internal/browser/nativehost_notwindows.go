//go:build !windows

package browser

// registerWindowsHosts is a no-op on non-Windows platforms (InstallNativeHost
// only calls it when GOOS == "windows"; this stub keeps the build green).
func registerWindowsHosts(manifestPath string) error { return nil }
