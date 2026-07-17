package computer

import (
	"runtime"
	"strconv"
	"strings"
)

const (
	// MinimumMacOSVersion is the deployment target used by the native Swift
	// helper and capture worker. Keep the user-facing requirement in one place.
	MinimumMacOSVersion = "14.0"
)

// Supported reports whether this process can offer native computer use.
// Computer use deliberately has no cross-platform fallback: production builds
// only drive the macOS Accessibility and Screen Recording helper.
func Supported() bool {
	if !SupportedPlatform(runtime.GOOS) {
		return false
	}

	productVersion, err := macOSProductVersion()
	return supportedRuntime(runtime.GOOS, productVersion, err)
}

// SupportedPlatform is the pure form used by platform-gating tests.
func SupportedPlatform(goos string) bool {
	return goos == "darwin"
}

// SupportedMacOSVersion reports whether productVersion satisfies the native
// helper's minimum deployment target. It is deliberately pure so the version
// policy can be covered on every CI platform.
func SupportedMacOSVersion(productVersion string) bool {
	return versionAtLeast(productVersion, MinimumMacOSVersion)
}

func supportedRuntime(goos, productVersion string, probeErr error) bool {
	return SupportedPlatform(goos) && probeErr == nil && SupportedMacOSVersion(productVersion)
}

func versionAtLeast(actual, minimum string) bool {
	actualParts, ok := parseVersion(actual)
	if !ok {
		return false
	}
	minimumParts, ok := parseVersion(minimum)
	if !ok {
		return false
	}

	componentCount := max(len(actualParts), len(minimumParts))
	for i := 0; i < componentCount; i++ {
		actualPart := versionPart(actualParts, i)
		minimumPart := versionPart(minimumParts, i)
		if actualPart != minimumPart {
			return actualPart > minimumPart
		}
	}
	return true
}

func parseVersion(version string) ([]int, bool) {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) == 0 {
		return nil, false
	}

	components := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		component, err := strconv.Atoi(part)
		if err != nil || component < 0 {
			return nil, false
		}
		components[i] = component
	}
	return components, true
}

func versionPart(parts []int, index int) int {
	if index >= len(parts) {
		return 0
	}
	return parts[index]
}

// UnsupportedReason returns a stable, actionable message for APIs and CLIs.
func UnsupportedReason() string {
	return "Computer Use requires macOS " + MinimumMacOSVersion + " or newer"
}
