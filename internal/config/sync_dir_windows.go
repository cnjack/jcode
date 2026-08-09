//go:build windows

package config

// Windows makes the same-volume replacement durable through the flushed file
// handle and atomic rename. Opening directories for FlushFileBuffers requires
// platform-specific backup privileges, so there is no additional directory
// sync step here.
func syncConfigDirectory(string) error { return nil }
