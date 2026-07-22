package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// newCloudSupervisor builds the cloud relay supervisor: it owns the connector
// lifecycle (start on `jcode web` boot, live stop/start via the settings
// auto_connect toggle) and the status served at GET /api/cloud/status. The
// relay is strictly best-effort: every failure is logged as a warning and it
// never blocks or fails the web server.
func newCloudSupervisor(cfg *config.Config, port int, webToken string) *cloud.Supervisor {
	sup := cloud.NewSupervisor(cfg, port, webToken)
	sup.Version = Version
	return sup
}

// --- `jcode cloud` command group (M5: pairing approval + CEK management) ---

// NewCloudCmd returns the `jcode cloud` command group: pairing approval and
// E2E key management against the jcloud orchestrator. Like `jcode login` it
// prints to plain stdout — no TUI involved.
func NewCloudCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Manage the jcloud device relay (pairings, E2E key, status)",
	}
	cmd.AddCommand(
		newCloudPairingsCmd(),
		newCloudApproveCmd(),
		newCloudDenyCmd(),
		newCloudStatusCmd(),
		newCloudKeyCmd(),
		newCloudRotateKeyCmd(),
		newCloudGuideCmd(),
	)
	return cmd
}

// newCloudGuideCmd prints the condensed quick-start (M7): the same core
// content as docs/cloud.md, for users who discover the feature from the CLI.
func newCloudGuideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print the cloud quick-start guide (condensed docs/cloud.md)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudGuide(cmd.OutOrStdout())
		},
	}
}

func runCloudGuide(w io.Writer) error {
	_, _ = fmt.Fprint(w, `jcode 云端（jcloud）快速上手
============================

1. 登录设备
   jcode login                 默认登录 https://cloud.j-code.net；浏览器确认后自动连接
   jcode login --cloud <url>   self-host 云端（必须 https；仅 localhost 开发允许 http）
   jcode login --status        查看当前登录状态
   jcode logout                退出登录：吊销设备令牌并清除本地凭据

2. 远程使用
   登录后，在 cloud 控制台（/devices）或手机 app 中选择设备：发起会话、
   实时查看、停止运行、处理权限审批。设备离线时可翻看历史，但不能操作。

3. 新客户端配对（端到端加密）
   会话内容端到端加密，云端只见密文。新浏览器/手机首次使用需配对：
   在客户端发起后，于设备上 10 分钟内批准。
   jcode cloud pairings        查看待批准的配对请求
   jcode cloud approve <id>    批准（拒绝对应 jcode cloud deny <id>）

4. 密钥与恢复
   jcode cloud key show-phrase 显示 24 词恢复短语（请离线妥善保存）
   jcode cloud key recover     全部设备丢失后，用短语恢复密钥
   jcode cloud rotate-key      更换密钥（已配对客户端需重新配对）

5. 状态与开关
   jcode cloud status          云端地址 / 设备 id / 密钥代数 / 连通性
   端到端加密默认开启；排查时可在 ~/.jcode/config.json 设置
   "cloud": { "e2ee": false } 并重启 jcode web（明文上行）。

完整文档见 docs/cloud.md。
`)
	return nil
}

func newCloudPairingsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pairings",
		Short: "List pending pairing requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudPairings(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newCloudApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <pairing_id>",
		Short: "Approve a pairing request (wraps the CEK for the requester)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudApprove(cmd.Context(), cmd.OutOrStdout(), args[0])
		},
	}
}

func newCloudDenyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deny <pairing_id>",
		Short: "Deny a pairing request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudDeny(cmd.Context(), cmd.OutOrStdout(), args[0])
		},
	}
}

func newCloudStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show cloud relay and E2E key status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudStatus(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newCloudKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage the account E2E encryption key (CEK)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show-phrase",
			Short: "Show the 24-word recovery phrase for the CEK",
			RunE: func(cmd *cobra.Command, args []string) error {
				return runCloudKeyShowPhrase(cmd.OutOrStdout(), cmd.InOrStdin())
			},
		},
		&cobra.Command{
			Use:   "recover",
			Short: "Rebuild the CEK from a 24-word recovery phrase",
			RunE: func(cmd *cobra.Command, args []string) error {
				return runCloudKeyRecover(cmd.OutOrStdout(), cmd.InOrStdin())
			},
		},
	)
	return cmd
}

func newCloudRotateKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-key",
		Short: "Generate a new CEK (key_gen+1); paired clients must re-pair",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudRotateKey(cmd.OutOrStdout(), cmd.InOrStdin())
		},
	}
}

// loadDeviceClient loads the device credentials and builds an orchestrator
// client. Errors when not logged in.
func loadDeviceClient() (*cloud.Credentials, *cloud.Client, error) {
	creds, err := cloud.LoadCredentials()
	if err != nil {
		return nil, nil, err
	}
	if creds == nil {
		return nil, nil, fmt.Errorf("not logged in: run `jcode login` first")
	}
	return creds, cloud.NewClient(creds.CloudURL), nil
}

// confirm prints prompt and returns true only when the user answers "yes".
func confirm(w io.Writer, in *bufio.Reader, prompt string) bool {
	_, _ = fmt.Fprintf(w, "%s [type 'yes' to continue]: ", prompt)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line) == "yes"
}

func runCloudPairings(ctx context.Context, w io.Writer) error {
	creds, client, err := loadDeviceClient()
	if err != nil {
		return err
	}
	pairings, err := client.ListPairings(ctx, creds.DeviceToken, "pending")
	if err != nil {
		return fmt.Errorf("list pairings: %w", err)
	}
	if len(pairings) == 0 {
		_, _ = fmt.Fprintln(w, "No pending pairing requests.")
		return nil
	}
	_, _ = fmt.Fprintln(w, "Pending pairing requests:")
	for _, p := range pairings {
		_, _ = fmt.Fprintf(w, "  %s  %-24s  %s\n", p.ID, p.Label, p.CreatedAt)
	}
	_, _ = fmt.Fprintln(w, "\nApprove with `jcode cloud approve <id>`, deny with `jcode cloud deny <id>`.")
	return nil
}

func runCloudApprove(ctx context.Context, w io.Writer, id string) error {
	creds, client, err := loadDeviceClient()
	if err != nil {
		return err
	}
	pairing, err := client.GetPairing(ctx, creds.DeviceToken, id)
	if err != nil {
		return fmt.Errorf("get pairing %s: %w", id, err)
	}
	if pairing.PubKey == "" {
		return fmt.Errorf("pairing %s has no requester pubkey", id)
	}
	cipher, err := cloud.EnsureCEK()
	if err != nil {
		return err
	}
	wrap, err := cloud.WrapCEK(pairing.PubKey, cipher.CEK(), cipher.KeyGen())
	if err != nil {
		return fmt.Errorf("wrap CEK for pairing %s: %w", id, err)
	}
	if err := client.RespondPairing(ctx, creds.DeviceToken, id, true, cipher.KeyGen(), wrap); err != nil {
		return fmt.Errorf("respond to pairing %s: %w", id, err)
	}
	_, _ = fmt.Fprintf(w, "Approved pairing %s (label %q) — CEK key_gen=%d wrapped for the requester.\n", id, pairing.Label, cipher.KeyGen())
	return nil
}

func runCloudDeny(ctx context.Context, w io.Writer, id string) error {
	creds, client, err := loadDeviceClient()
	if err != nil {
		return err
	}
	if err := client.RespondPairing(ctx, creds.DeviceToken, id, false, 0, nil); err != nil {
		return fmt.Errorf("respond to pairing %s: %w", id, err)
	}
	_, _ = fmt.Fprintf(w, "Denied pairing %s.\n", id)
	return nil
}

func runCloudStatus(ctx context.Context, w io.Writer) error {
	creds, client, err := loadDeviceClient()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "cloud url:   %s\n", creds.CloudURL)
	_, _ = fmt.Fprintf(w, "device name: %s\n", creds.DeviceName)
	_, _ = fmt.Fprintf(w, "device id:   %s\n", creds.DeviceID)
	_, _ = fmt.Fprintf(w, "key gen:     %d\n", creds.KeyGen)
	cekStatus := "not initialized (plaintext uplink)"
	if creds.CEK != "" {
		cekStatus = "initialized (E2E encryption active)"
	}
	_, _ = fmt.Fprintf(w, "cek:         %s\n", cekStatus)
	if err := client.Heartbeat(ctx, creds.DeviceToken); err != nil {
		_, _ = fmt.Fprintf(w, "online:      no (heartbeat failed: %v)\n", err)
	} else {
		_, _ = fmt.Fprintln(w, "online:      yes (heartbeat ok)")
	}
	return nil
}

func runCloudKeyShowPhrase(w io.Writer, in io.Reader) error {
	cipher, err := cloud.EnsureCEK()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(in)
	_, _ = fmt.Fprintln(w, "WARNING: the recovery phrase decrypts ALL synced content of this account.")
	_, _ = fmt.Fprintln(w, "Anyone who sees it can read your sessions. Do not share it, store it safely.")
	if !confirm(w, reader, "Reveal the 24-word recovery phrase?") {
		_, _ = fmt.Fprintln(w, "Aborted.")
		return nil
	}
	phrase, err := cloud.CEKToPhrase(cipher.CEK())
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\nRecovery phrase (key_gen=%d):\n\n    %s\n\n", cipher.KeyGen(), phrase)
	_, _ = fmt.Fprintln(w, "Write it down and keep it offline. It is NOT stored anywhere else.")
	return nil
}

func runCloudKeyRecover(w io.Writer, in io.Reader) error {
	if _, _, err := loadDeviceClient(); err != nil {
		return err
	}
	reader := bufio.NewReader(in)
	_, _ = fmt.Fprintln(w, "Paste the 24-word recovery phrase (single line, space separated):")
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("read recovery phrase: %w", err)
	}
	phrase := strings.TrimSpace(line)
	if _, err := cloud.CEKFromPhrase(phrase); err != nil {
		return err
	}
	if creds, err := cloud.LoadCredentials(); err != nil {
		return err
	} else if creds != nil && creds.CEK != "" {
		_, _ = fmt.Fprintln(w, "WARNING: a CEK already exists on this device. Recovering OVERWRITES it.")
		_, _ = fmt.Fprintln(w, "Content encrypted with the current key becomes unreadable for this device.")
		if !confirm(w, reader, "Overwrite the existing CEK with the recovered one?") {
			_, _ = fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}
	cipher, err := cloud.RecoverCEK(phrase)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "CEK recovered (key_gen=%d). Uplink is now encrypted with the recovered key.\n", cipher.KeyGen())
	return nil
}

func runCloudRotateKey(w io.Writer, in io.Reader) error {
	if _, _, err := loadDeviceClient(); err != nil {
		return err
	}
	reader := bufio.NewReader(in)
	_, _ = fmt.Fprintln(w, "Rotating the CEK generates a new key (key_gen+1).")
	_, _ = fmt.Fprintln(w, "Already-paired clients keep the old key and CANNOT read new content until they re-pair.")
	if !confirm(w, reader, "Rotate the CEK now?") {
		_, _ = fmt.Fprintln(w, "Aborted.")
		return nil
	}
	cipher, err := cloud.RotateCEK()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "CEK rotated. New key_gen=%d.\n", cipher.KeyGen())
	_, _ = fmt.Fprintln(w, "Note: authorized clients (console / mobile) must pair again to receive the new key.")
	_, _ = fmt.Fprintln(w, "Consider saving the new recovery phrase: `jcode cloud key show-phrase`.")
	return nil
}
