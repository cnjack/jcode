//go:build windows

package browser

import "golang.org/x/sys/windows/registry"

// registerWindowsHosts points Chrome and Edge at the native-host manifest via
// per-user registry keys (HKCU, no admin needed).
func registerWindowsHosts(manifestPath string) error {
	subkeys := []string{
		`Software\Google\Chrome\NativeMessagingHosts\` + NativeHostName,
		`Software\Microsoft\Edge\NativeMessagingHosts\` + NativeHostName,
		`Software\Chromium\NativeMessagingHosts\` + NativeHostName,
	}
	var firstErr error
	for _, sk := range subkeys {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, sk, registry.WRITE)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// The (Default) value must be the absolute path to the manifest JSON.
		if err := k.SetStringValue("", manifestPath); err != nil && firstErr == nil {
			firstErr = err
		}
		_ = k.Close()
	}
	return firstErr
}
