//go:build !darwin

package computer

import "errors"

func macOSProductVersion() (string, error) {
	return "", errors.New("macOS product-version probe is unavailable")
}
