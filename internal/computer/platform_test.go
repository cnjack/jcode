package computer

import (
	"errors"
	"strings"
	"testing"
)

func TestSupportedPlatformIsMacOSOnly(t *testing.T) {
	for _, tc := range []struct {
		goos string
		want bool
	}{
		{goos: "darwin", want: true},
		{goos: "linux", want: false},
		{goos: "windows", want: false},
		{goos: "freebsd", want: false},
	} {
		if got := SupportedPlatform(tc.goos); got != tc.want {
			t.Errorf("SupportedPlatform(%q) = %v, want %v", tc.goos, got, tc.want)
		}
	}
}

func TestSupportedMacOSVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		want    bool
	}{
		{name: "minimum", version: "14.0", want: true},
		{name: "minimum without minor", version: "14", want: true},
		{name: "newer minor", version: "14.1", want: true},
		{name: "newer major", version: "15.0", want: true},
		{name: "newer patch", version: "14.0.1", want: true},
		{name: "older", version: "13.6.9", want: false},
		{name: "empty", version: "", want: false},
		{name: "malformed", version: "14.beta", want: false},
		{name: "empty component", version: "14..1", want: false},
		{name: "negative", version: "-14.0", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SupportedMacOSVersion(tc.version); got != tc.want {
				t.Fatalf("SupportedMacOSVersion(%q) = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}

func TestSupportedRuntimeFailsClosed(t *testing.T) {
	probeErr := errors.New("sysctl failed")
	for _, tc := range []struct {
		name           string
		goos           string
		productVersion string
		probeErr       error
		want           bool
	}{
		{name: "supported macOS", goos: "darwin", productVersion: "14.0", want: true},
		{name: "older macOS", goos: "darwin", productVersion: "13.6", want: false},
		{name: "probe failure", goos: "darwin", productVersion: "99.0", probeErr: probeErr, want: false},
		{name: "non macOS", goos: "linux", productVersion: "99.0", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := supportedRuntime(tc.goos, tc.productVersion, tc.probeErr); got != tc.want {
				t.Fatalf("supportedRuntime(%q, %q, %v) = %v, want %v", tc.goos, tc.productVersion, tc.probeErr, got, tc.want)
			}
		})
	}
}

func TestUnsupportedReasonMatchesMinimumVersion(t *testing.T) {
	if reason := UnsupportedReason(); !strings.Contains(reason, MinimumMacOSVersion) {
		t.Fatalf("UnsupportedReason() = %q, want minimum version %q", reason, MinimumMacOSVersion)
	}
}
