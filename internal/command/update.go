package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func NewUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update jcode to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate()
		},
	}
}

func runUpdate() error {
	currentVersion := Version

	fmt.Println("Checking for updates...")

	latest, err := getLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check latest version: %w", err)
	}

	if normalizeVersion(latest) == normalizeVersion(currentVersion) {
		fmt.Printf("Already up to date: %s\n", currentVersion)
		return nil
	}

	fmt.Printf("Update available: %s -> %s\n", currentVersion, latest)

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	suffix := ""
	if goos == "windows" {
		suffix = ".exe"
	}

	filename := fmt.Sprintf("jcode-%s-%s%s", goos, goarch, suffix)
	downloadURL := fmt.Sprintf("https://github.com/cnjack/jcode/releases/download/%s/%s", latest, filename)

	fmt.Printf("Downloading %s...\n", downloadURL)

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine current binary path: %w", err)
	}

	// Resolve symlinks to get the actual binary path
	resolved, err := resolveExecutablePath(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve binary path: %w", err)
	}
	execPath = resolved

	tmpFile, err := os.CreateTemp("", "jcode-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	resp, err := http.Get(downloadURL) // #nosec G107 -- URL is constructed from hardcoded GitHub domain
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_ = tmpFile.Close()
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to save download: %w", err)
	}
	_ = tmpFile.Close()

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	if goos == "windows" {
		// On Windows, the running executable is locked and cannot be renamed or overwritten.
		// Download the new version to a temp file and instruct the user to replace manually.
		dstPath := execPath + ".new"
		if err := copyFile(tmpPath, dstPath); err != nil {
			return fmt.Errorf("failed to save new version: %w", err)
		}
		fmt.Printf("Update downloaded: %s -> %s (pending manual replacement)\n", currentVersion, latest)
		fmt.Println()
		fmt.Println("Windows cannot replace a running executable.")
		fmt.Println("To finish the update, please:")
		fmt.Printf("  1. Exit jcode\n")
		fmt.Printf("  2. Run: move /Y \"%s\" \"%s\"\n", dstPath, execPath)
		fmt.Println("  3. Restart jcode")
		fmt.Println()
		fmt.Printf("New binary saved to: %s\n", dstPath)
		return nil
	}

	// Replace the current binary
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Cross-device rename; fall back to copy
		if err := copyFile(tmpPath, execPath); err != nil {
			return fmt.Errorf("failed to replace binary at %s: %w", execPath, err)
		}
	}

	fmt.Printf("Updated successfully: %s -> %s\n", currentVersion, latest)
	fmt.Printf("Binary: %s\n", execPath)
	return nil
}

func getLatestVersion() (string, error) {
	resp, err := http.Get("https://api.github.com/repos/cnjack/jcode/releases/latest") // #nosec G107
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return release.TagName, nil
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

func resolveExecutablePath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return path, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		return resolved, nil
	}
	return path, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}
