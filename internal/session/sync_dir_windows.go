//go:build windows

package session

// Windows makes a same-volume replacement durable through the flushed file
// handle and atomic rename. Opening directories for FlushFileBuffers requires
// platform-specific backup privileges, so there is no additional directory
// sync step here.
func syncSessionDirectory(string) error { return nil }
