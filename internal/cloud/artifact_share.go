package cloud

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ArtifactShareProtocolV1 = "jcode-artifact-share-v1"
	artifactShareMaxSize    = 25 << 20
	artifactShareMinExpiry  = time.Hour
	artifactShareMaxExpiry  = 30 * 24 * time.Hour
)

// ArtifactShareMetadata is encrypted locally. Cloud only receives its opaque
// AES-GCM envelope and therefore cannot read the title, path, type, or size.
type ArtifactShareMetadata struct {
	Title        string `json:"title"`
	RelativePath string `json:"relative_path"`
	MediaType    string `json:"media_type"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
}

type ArtifactShareInput struct {
	ArtifactID   string
	Revision     int
	Title        string
	RelativePath string
	MediaType    string
	Kind         string
	Content      []byte
	ExpiresIn    time.Duration
}

type ArtifactShareResult struct {
	ShareID   string    `json:"share_id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ArtifactShareSummary struct {
	ShareID          string     `json:"share_id"`
	ArtifactID       string     `json:"artifact_id"`
	Revision         int        `json:"revision"`
	State            string     `json:"state"`
	CiphertextSize   int64      `json:"ciphertext_size"`
	CiphertextSHA256 string     `json:"ciphertext_sha256,omitempty"`
	ExpiresAt        time.Time  `json:"expires_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// ArtifactSharePublisher performs the explicit device-authenticated upload.
// It is independent of relay connectivity: valid login credentials are enough.
type ArtifactSharePublisher struct {
	httpClient *http.Client
	rand       io.Reader
}

func NewArtifactSharePublisher(httpClient *http.Client) *ArtifactSharePublisher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	return &ArtifactSharePublisher{httpClient: httpClient, rand: rand.Reader}
}

type artifactShareIntent struct {
	ShareID     string    `json:"share_id"`
	UploadURL   string    `json:"upload_url"`
	CompleteURL string    `json:"complete_url"`
	BaseURL     string    `json:"base_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type encryptedArtifactMetadata struct {
	Nonce           string `json:"nonce"`
	Ciphertext      string `json:"ciphertext"`
	PlaintextLength int64  `json:"plaintext_length"`
}

func (p *ArtifactSharePublisher) Publish(ctx context.Context, creds *Credentials, input ArtifactShareInput) (_ *ArtifactShareResult, retErr error) {
	client, token, err := p.client(creds)
	if err != nil {
		return nil, err
	}
	if !validArtifactShareID(input.ArtifactID) || input.Revision <= 0 || strings.TrimSpace(input.Title) == "" ||
		strings.TrimSpace(input.MediaType) == "" || strings.TrimSpace(input.Kind) == "" {
		return nil, fmt.Errorf("artifact share input is invalid")
	}
	if len(input.Content) > artifactShareMaxSize {
		return nil, fmt.Errorf("artifact exceeds the 25 MiB share limit")
	}
	if input.ExpiresIn == 0 {
		input.ExpiresIn = 7 * 24 * time.Hour
	}
	if input.ExpiresIn < artifactShareMinExpiry || input.ExpiresIn > artifactShareMaxExpiry {
		return nil, fmt.Errorf("artifact share expiry must be between 1 hour and 30 days")
	}
	// Freeze caller-owned bytes before creating remote state.
	content := bytes.Clone(input.Content)
	key := make([]byte, 32)
	if _, err := io.ReadFull(p.rand, key); err != nil {
		return nil, fmt.Errorf("generate artifact share key: %w", err)
	}

	var intent artifactShareIntent
	err = client.post(ctx, "/internal/v1/device/artifact-shares/intents", token, map[string]any{
		"protocol": ArtifactShareProtocolV1, "artifact_id": input.ArtifactID,
		"revision": input.Revision, "ciphertext_size": len(content) + 28,
		"expires_in_seconds": int64(input.ExpiresIn / time.Second),
	}, &intent)
	if err != nil {
		return nil, fmt.Errorf("create artifact share intent: %w", err)
	}
	if !validArtifactShareID(intent.ShareID) {
		return nil, fmt.Errorf("artifact share service returned an invalid share id")
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = p.revoke(cleanupCtx, client, token, intent.ShareID)
	}()
	wantUpload := "/internal/v1/device/artifact-shares/" + intent.ShareID + "/content"
	wantComplete := "/internal/v1/device/artifact-shares/" + intent.ShareID + "/complete"
	if intent.UploadURL != wantUpload || intent.CompleteURL != wantComplete {
		return nil, fmt.Errorf("artifact share service returned invalid upload endpoints")
	}

	contentNonce, contentCiphertext, err := p.seal(key, content,
		artifactShareAAD(intent.ShareID, "content", input.ArtifactID, input.Revision, int64(len(content))))
	if err != nil {
		return nil, err
	}
	contentWire := append(bytes.Clone(contentNonce), contentCiphertext...)
	digest := sha256.Sum256(contentWire)

	metadataPlaintext, err := json.Marshal(ArtifactShareMetadata{
		Title: input.Title, RelativePath: input.RelativePath, MediaType: input.MediaType,
		Kind: input.Kind, Size: int64(len(content)),
	})
	if err != nil {
		return nil, fmt.Errorf("encode artifact share metadata: %w", err)
	}
	metadataNonce, metadataCiphertext, err := p.seal(key, metadataPlaintext,
		artifactShareAAD(intent.ShareID, "metadata", input.ArtifactID, input.Revision, int64(len(metadataPlaintext))))
	if err != nil {
		return nil, err
	}

	if err := p.upload(ctx, client, token, wantUpload, contentWire); err != nil {
		return nil, fmt.Errorf("upload encrypted artifact: %w", err)
	}
	var complete ArtifactShareSummary
	if err := client.post(ctx, wantComplete, token, map[string]any{
		"ciphertext_sha256": hex.EncodeToString(digest[:]),
		"encrypted_metadata": encryptedArtifactMetadata{
			Nonce:           base64.RawURLEncoding.EncodeToString(metadataNonce),
			Ciphertext:      base64.RawURLEncoding.EncodeToString(metadataCiphertext),
			PlaintextLength: int64(len(metadataPlaintext)),
		},
	}, &complete); err != nil {
		return nil, fmt.Errorf("complete artifact share: %w", err)
	}
	shareURL, err := artifactShareURL(intent.BaseURL, key)
	if err != nil {
		return nil, err
	}
	completed = true
	return &ArtifactShareResult{ShareID: intent.ShareID, URL: shareURL, ExpiresAt: intent.ExpiresAt}, nil
}

func (p *ArtifactSharePublisher) List(ctx context.Context, creds *Credentials, artifactID string) ([]ArtifactShareSummary, error) {
	client, token, err := p.client(creds)
	if err != nil {
		return nil, err
	}
	path := "/internal/v1/device/artifact-shares"
	if artifactID != "" {
		if !validArtifactShareID(artifactID) {
			return nil, fmt.Errorf("artifact id is invalid")
		}
		path += "?artifact_id=" + url.QueryEscape(artifactID)
	}
	var out struct {
		Shares []ArtifactShareSummary `json:"shares"`
	}
	if _, err := client.get(ctx, path, token, &out); err != nil {
		return nil, err
	}
	return out.Shares, nil
}

func (p *ArtifactSharePublisher) Revoke(ctx context.Context, creds *Credentials, shareID string) error {
	client, token, err := p.client(creds)
	if err != nil {
		return err
	}
	return p.revoke(ctx, client, token, shareID)
}

func (p *ArtifactSharePublisher) client(creds *Credentials) (*Client, string, error) {
	if creds == nil || strings.TrimSpace(creds.DeviceToken) == "" {
		return nil, "", fmt.Errorf("cloud login is required")
	}
	baseURL, err := ValidateCloudURL(creds.CloudURL)
	if err != nil {
		return nil, "", err
	}
	client := NewClient(baseURL)
	client.HTTPClient = p.httpClient
	return client, creds.DeviceToken, nil
}

func (p *ArtifactSharePublisher) upload(ctx context.Context, client *Client, token, path string, content []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, client.BaseURL+path, bytes.NewReader(content))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(content))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return nil
}

func (p *ArtifactSharePublisher) revoke(ctx context.Context, client *Client, token, shareID string) error {
	if !validArtifactShareID(shareID) {
		return fmt.Errorf("artifact share id is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		client.BaseURL+"/internal/v1/device/artifact-shares/"+shareID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("revoke artifact share: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (p *ArtifactSharePublisher) seal(key, plaintext, aad []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(p.rand, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate artifact share nonce: %w", err)
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, aad), nil
}

func artifactShareAAD(shareID, part, artifactID string, revision int, plaintextLength int64) []byte {
	return []byte(ArtifactShareProtocolV1 + "\n" + shareID + "\n" + part + "\n" + artifactID + "\n" +
		strconv.Itoa(revision) + "\n" + strconv.FormatInt(plaintextLength, 10))
}

func artifactShareURL(baseURL string, key []byte) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("artifact share service returned an invalid public URL")
	}
	u.Fragment = "k=v1." + base64.RawURLEncoding.EncodeToString(key)
	return u.String(), nil
}

func validArtifactShareID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i := range len(value) {
		char := value[i]
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
