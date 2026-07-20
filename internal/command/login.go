package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// NewLoginCmd returns the `jcode login` command: RFC 8628 device-code sign-in
// to jcloud (see cloud/docs/17-jcode-device-relay.md §3). It prints to plain
// stdout like `jcode update` / `jcode sessions` — no TUI involved.
func NewLoginCmd() *cobra.Command {
	var cloudURL, name string
	var status bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to jcloud (device code flow)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if status {
				return runLoginStatus()
			}
			return runLogin(cmd.Context(), cloudURL, name)
		},
	}
	cmd.Flags().StringVar(&cloudURL, "cloud", cloud.DefaultCloudURL, "jcloud orchestrator URL (https; http allowed only for localhost)")
	cmd.Flags().StringVar(&name, "name", "", "device name shown in jcloud (defaults to hostname)")
	cmd.Flags().BoolVar(&status, "status", false, "show current login status and exit")
	return cmd
}

// NewLogoutCmd returns the `jcode logout` command: revoke the device token
// remotely (best effort) and clear local credentials.
func NewLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign out of jcloud and remove local device credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(cmd.Context())
		},
	}
}

// openBrowser opens url in the system browser. It is a variable so tests can
// stub it out.
var openBrowser = func(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func runLogin(ctx context.Context, rawURL, name string) error {
	baseURL, err := cloud.ValidateCloudURL(rawURL)
	if err != nil {
		return err
	}
	if existing, err := cloud.LoadCredentials(); err != nil {
		return err
	} else if existing != nil {
		fmt.Printf("Already logged in to %s as device %q (%s).\n", existing.CloudURL, existing.DeviceName, existing.DeviceID)
		fmt.Println("Run `jcode logout` first to sign in again.")
		return nil
	}

	hostname, _ := os.Hostname()
	if name == "" {
		name = hostname
	}
	if name == "" {
		name = "jcode-device"
	}

	client := cloud.NewClient(baseURL)

	dc, err := client.RequestDeviceCode(ctx, "jcode CLI "+Version)
	if err != nil {
		return fmt.Errorf("failed to request device code: %w", err)
	}

	fmt.Println()
	fmt.Println("To sign in to jcloud, open this page in your browser:")
	fmt.Println()
	fmt.Printf("    %s\n", dc.VerificationURI)
	fmt.Println()
	fmt.Println("and enter this code:")
	fmt.Println()
	fmt.Printf("    %s\n", dc.UserCode)
	fmt.Println()
	if err := openBrowser(dc.VerificationURI); err != nil {
		fmt.Printf("(could not open a browser automatically: %v — please open the URL manually)\n", err)
	}
	fmt.Println("Waiting for authorization... (Ctrl+C to cancel)")

	interval := time.Duration(dc.Interval) * time.Second
	expiresIn := time.Duration(dc.ExpiresIn) * time.Second
	tok, err := client.PollForToken(ctx, dc.DeviceCode, interval, expiresIn)
	if err != nil {
		switch {
		case errors.Is(err, cloud.ErrAuthorizationDenied):
			return fmt.Errorf("login failed: %w", err)
		case errors.Is(err, cloud.ErrDeviceCodeExpired):
			return fmt.Errorf("login failed: %w — run `jcode login` to try again", err)
		default:
			return fmt.Errorf("login failed while waiting for authorization: %w", err)
		}
	}

	pubKey, privKey, err := cloud.GenerateIdentityKeyPair()
	if err != nil {
		return err
	}

	if err := client.RegisterDevice(ctx, tok.AccessToken, cloud.RegisterDeviceRequest{
		Name:         name,
		Hostname:     hostname,
		JcodeVersion: Version,
		PubKey:       pubKey,
	}); err != nil {
		return fmt.Errorf("failed to register device: %w", err)
	}

	creds := &cloud.Credentials{
		CloudURL:    baseURL,
		DeviceID:    tok.DeviceID,
		DeviceToken: tok.AccessToken,
		DeviceName:  name,
		PublicKey:   pubKey,
		PrivateKey:  privKey,
		KeyGen:      1,
	}
	if err := cloud.SaveCredentials(creds); err != nil {
		return err
	}

	if err := updateConfigCloud(baseURL, true); err != nil {
		fmt.Printf("Warning: failed to update %s: %v\n", config.ConfigPath(), err)
	}

	fmt.Println()
	fmt.Printf("Signed in to %s as device %q (%s).\n", baseURL, name, tok.DeviceID)
	fmt.Printf("Credentials saved to %s\n", credentialsPathForDisplay())
	return nil
}

func runLogout(ctx context.Context) error {
	creds, err := cloud.LoadCredentials()
	if err != nil {
		return err
	}
	if creds == nil {
		fmt.Println("Not logged in.")
		return nil
	}

	// Remote revocation is best effort: the revoke endpoint may not exist yet
	// on the orchestrator, and a network failure must not trap the local
	// credentials on disk.
	client := cloud.NewClient(creds.CloudURL)
	if err := client.RevokeDevice(ctx, creds.DeviceToken); err != nil {
		fmt.Printf("Warning: failed to revoke device token on %s: %v\n", creds.CloudURL, err)
		fmt.Println("Clearing local credentials anyway.")
	}

	if err := cloud.DeleteCredentials(); err != nil {
		return err
	}

	if err := updateConfigCloud("", false); err != nil {
		fmt.Printf("Warning: failed to update %s: %v\n", config.ConfigPath(), err)
	}

	fmt.Println("Logged out. Local device credentials removed.")
	return nil
}

func runLoginStatus() error {
	creds, err := cloud.LoadCredentials()
	if err != nil {
		return err
	}
	if creds == nil {
		fmt.Println("Not logged in. Run `jcode login` to sign in.")
		return nil
	}
	fmt.Printf("cloud url:   %s\n", creds.CloudURL)
	fmt.Printf("device name: %s\n", creds.DeviceName)
	fmt.Printf("device id:   %s\n", creds.DeviceID)
	fmt.Printf("key gen:     %d\n", creds.KeyGen)
	return nil
}

// updateConfigCloud sets config.cloud while preserving the stored url (when
// the url argument is empty, i.e. logout) and the user's auto_connect
// preference. Login/logout must not require a fully configured provider set,
// so a LoadConfig failure falls back to a best-effort raw read of the file
// (unknown fields may be dropped in that case).
func updateConfigCloud(url string, enabled bool) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = &config.Config{}
		if data, readErr := os.ReadFile(config.ConfigPath()); readErr == nil {
			_ = json.Unmarshal(data, cfg)
		}
	}
	current := cfg.CloudSettings()
	if url == "" {
		url = current.URL
	}
	cfg.SetCloud(&config.CloudConfig{Enabled: enabled, URL: url, AutoConnect: current.AutoConnect})
	return config.SaveConfig(cfg)
}

func credentialsPathForDisplay() string {
	path, err := cloud.CredentialsPath()
	if err != nil {
		return "~/.jcode/cloud.json"
	}
	return path
}
