// inbox.go lands non-image chat attachments (M12) at the fixed per-session
// location ~/.jcode/inbox/<session_id>/<filename>. The message text then
// references them ("[附件] name → path") so the agent can read them with its
// file tools. Attachment bytes travel inside the E2E envelope, so they are
// only ever plaintext on-device.
package cloud

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	// maxAttachmentBytes is the per-attachment decoded size limit (2MB),
	// enforced connector-side per the M12 contract.
	maxAttachmentBytes = 2 << 20
	// maxAttachmentCount is the per-command attachment count limit.
	maxAttachmentCount = 5

	inboxDirMode  = 0o700
	inboxFileMode = 0o600
)

// chatAttachment is one non-image attachment in a chat.send payload.
type chatAttachment struct {
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	DataB64 string `json:"data_b64"`
}

// attachmentRef is one landed attachment, for the message reference list.
type attachmentRef struct {
	Name string
	Path string
}

// decodeAttachments enforces the count/size limits and decodes every
// attachment BEFORE any side effect, so a breach acks error without touching
// the session, the config, or the filesystem.
func decodeAttachments(atts []chatAttachment) ([][]byte, error) {
	if len(atts) > maxAttachmentCount {
		return nil, fmt.Errorf("attachments: %d files exceed the %d-file limit", len(atts), maxAttachmentCount)
	}
	decoded := make([][]byte, len(atts))
	for i, a := range atts {
		data, err := base64.StdEncoding.DecodeString(a.DataB64)
		if err != nil {
			// Tolerate unpadded base64.
			data, err = base64.RawStdEncoding.DecodeString(a.DataB64)
			if err != nil {
				return nil, fmt.Errorf("attachment %q: invalid base64 data", a.Name)
			}
		}
		if len(data) > maxAttachmentBytes {
			return nil, fmt.Errorf("attachment %q: %d bytes exceed the 2MB limit", a.Name, len(data))
		}
		decoded[i] = data
	}
	return decoded, nil
}

// sanitizeInboxName strips anything that could escape the inbox directory or
// create hidden/confusing files: path separators (both kinds), leading dots
// ("..", ".env"), and control characters. An empty result falls back to
// "attachment".
func sanitizeInboxName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(strings.TrimLeft(b.String(), "."))
	if name == "" {
		return "attachment"
	}
	return name
}

// writeInboxAttachments writes decoded attachments under root/<sessionID>/,
// creating the directory 0700 and files 0600. Name collisions (including
// pre-existing files) get a numeric suffix ("report.pdf" → "report-2.pdf").
// The session id is sanitized too — it is normally a server-minted UUID, but
// it crosses the trust boundary inside the command envelope. Returns the
// reference list in input order.
func writeInboxAttachments(root, sessionID string, atts []chatAttachment, decoded [][]byte) ([]attachmentRef, error) {
	dir := filepath.Join(root, sanitizeInboxName(sessionID))
	if err := os.MkdirAll(dir, inboxDirMode); err != nil {
		return nil, fmt.Errorf("create inbox dir: %w", err)
	}
	// MkdirAll does not fix the mode of a pre-existing dir.
	if err := os.Chmod(dir, inboxDirMode); err != nil {
		return nil, fmt.Errorf("secure inbox dir: %w", err)
	}
	refs := make([]attachmentRef, 0, len(atts))
	for i, a := range atts {
		name := sanitizeInboxName(a.Name)
		path := filepath.Join(dir, name)
		for n := 2; ; n++ {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				break
			}
			path = filepath.Join(dir, suffixedName(name, n))
		}
		if err := os.WriteFile(path, decoded[i], inboxFileMode); err != nil {
			return nil, fmt.Errorf("write attachment %q: %w", a.Name, err)
		}
		_ = os.Chmod(path, inboxFileMode) // umask may have cleared bits
		refs = append(refs, attachmentRef{Name: filepath.Base(path), Path: path})
	}
	return refs, nil
}

// suffixedName inserts "-n" before the extension: "report.pdf" → "report-2.pdf".
func suffixedName(name string, n int) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%d%s", base, n, ext)
}

// attachmentReferenceList renders the reference lines appended to the message
// text: "[附件] name → path", one per line.
func attachmentReferenceList(refs []attachmentRef) string {
	var b strings.Builder
	for _, r := range refs {
		fmt.Fprintf(&b, "\n[附件] %s → %s", r.Name, r.Path)
	}
	return b.String()
}
