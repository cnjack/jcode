package cloud

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestArtifactShareCanonicalCrossRuntimeVector(t *testing.T) {
	raw, err := os.ReadFile("../../../cloud/shared/artifact-share-v1.json")
	if err != nil {
		t.Skipf("cloud artifact vector is not available: %v", err)
	}
	var vector struct {
		Protocol           string `json:"protocol"`
		ShareID            string `json:"share_id"`
		ArtifactID         string `json:"artifact_id"`
		Revision           int    `json:"revision"`
		Key                string `json:"key_b64url"`
		MetadataPlaintext  string `json:"metadata_plaintext"`
		MetadataNonce      string `json:"metadata_nonce_b64url"`
		MetadataCiphertext string `json:"metadata_ciphertext_b64url"`
		ContentPlaintext   string `json:"content_plaintext_b64url"`
		ContentWire        string `json:"content_wire_b64url"`
		ContentWireSHA256  string `json:"content_wire_sha256"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	decode := func(value string) []byte {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(value)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return decoded
	}
	key := decode(vector.Key)
	metadataNonce := decode(vector.MetadataNonce)
	metadataCiphertext := decode(vector.MetadataCiphertext)
	metadata := openTestEnvelope(t, key, metadataNonce, metadataCiphertext,
		artifactShareAAD(vector.ShareID, "metadata", vector.ArtifactID, vector.Revision, int64(len(vector.MetadataPlaintext))))
	if string(metadata) != vector.MetadataPlaintext {
		t.Fatalf("metadata = %s", metadata)
	}
	var metadataValue ArtifactShareMetadata
	if err := json.Unmarshal(metadata, &metadataValue); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(metadataValue)
	if string(canonical) != vector.MetadataPlaintext {
		t.Fatalf("metadata encoding = %s", canonical)
	}

	wire := decode(vector.ContentWire)
	if len(wire) < 28 {
		t.Fatal("content wire is too short")
	}
	contentWant := decode(vector.ContentPlaintext)
	content := openTestEnvelope(t, key, wire[:12], wire[12:],
		artifactShareAAD(vector.ShareID, "content", vector.ArtifactID, vector.Revision, int64(len(contentWant))))
	if string(content) != string(contentWant) {
		t.Fatalf("content = %q", content)
	}
	digest := sha256.Sum256(wire)
	if hex.EncodeToString(digest[:]) != vector.ContentWireSHA256 {
		t.Fatalf("digest mismatch")
	}
}

func TestArtifactSharePublisherEncryptsContentAndMetadataWithFragmentKey(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var intentBody, uploaded, completeBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/device/artifact-shares/intents", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer device-token" {
			t.Fatalf("intent authorization = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		intentBody = body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"share_id":"share-123","upload_url":"/internal/v1/device/artifact-shares/share-123/content","complete_url":"/internal/v1/device/artifact-shares/share-123/complete","base_url":"https://share.example/s/share-123","expires_at":"2026-08-08T00:00:00Z"}`)
	})
	mux.HandleFunc("PUT /internal/v1/device/artifact-shares/share-123/content", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer device-token" {
			t.Fatalf("upload authorization = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		uploaded = body
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /internal/v1/device/artifact-shares/share-123/complete", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		completeBody = body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"share_id":"share-123","state":"complete","artifact_id":"artifact_1","revision":2,"expires_at":"2026-08-08T00:00:00Z"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	random := make([]byte, 32+12+12)
	for i := range random {
		random[i] = byte(i + 1)
	}
	publisher := NewArtifactSharePublisher(server.Client())
	publisher.rand = strings.NewReader(string(random))
	input := ArtifactShareInput{
		ArtifactID: "artifact_1", Revision: 2, Title: "Private benchmark",
		RelativePath: "reports/private.html", MediaType: "text/html", Kind: "html",
		Content: []byte("<h1>secret result</h1>"), ExpiresIn: 7 * 24 * time.Hour,
	}
	result, err := publisher.Publish(context.Background(), &Credentials{
		CloudURL: server.URL, DeviceToken: "device-token",
	}, input)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.ShareID != "share-123" || result.URL != "https://share.example/s/share-123#k=v1."+base64.RawURLEncoding.EncodeToString(random[:32]) {
		t.Fatalf("result = %#v", result)
	}

	mu.Lock()
	defer mu.Unlock()
	for name, body := range map[string][]byte{"intent": intentBody, "upload": uploaded, "complete": completeBody} {
		if strings.Contains(string(body), "Private benchmark") || strings.Contains(string(body), "secret result") || strings.Contains(string(body), "reports/private") {
			t.Fatalf("%s leaked plaintext: %s", name, body)
		}
	}
	var intent struct {
		Protocol       string `json:"protocol"`
		ArtifactID     string `json:"artifact_id"`
		Revision       int    `json:"revision"`
		CiphertextSize int64  `json:"ciphertext_size"`
	}
	if err := json.Unmarshal(intentBody, &intent); err != nil {
		t.Fatal(err)
	}
	if intent.Protocol != ArtifactShareProtocolV1 || intent.ArtifactID != input.ArtifactID || intent.Revision != 2 || intent.CiphertextSize != int64(len(input.Content)+28) {
		t.Fatalf("intent = %#v", intent)
	}

	key := random[:32]
	content := openTestEnvelope(t, key, uploaded[:12], uploaded[12:], artifactShareAAD("share-123", "content", input.ArtifactID, input.Revision, int64(len(input.Content))))
	if string(content) != string(input.Content) {
		t.Fatalf("content = %q", content)
	}
	var complete struct {
		CiphertextSHA256  string `json:"ciphertext_sha256"`
		EncryptedMetadata struct {
			Nonce           string `json:"nonce"`
			Ciphertext      string `json:"ciphertext"`
			PlaintextLength int64  `json:"plaintext_length"`
		} `json:"encrypted_metadata"`
	}
	if err := json.Unmarshal(completeBody, &complete); err != nil {
		t.Fatal(err)
	}
	nonce, _ := base64.RawURLEncoding.DecodeString(complete.EncryptedMetadata.Nonce)
	ciphertext, _ := base64.RawURLEncoding.DecodeString(complete.EncryptedMetadata.Ciphertext)
	metadata := openTestEnvelope(t, key, nonce, ciphertext, artifactShareAAD("share-123", "metadata", input.ArtifactID, input.Revision, complete.EncryptedMetadata.PlaintextLength))
	var decoded ArtifactShareMetadata
	if err := json.Unmarshal(metadata, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Title != input.Title || decoded.RelativePath != input.RelativePath || decoded.Size != int64(len(input.Content)) {
		t.Fatalf("metadata = %#v", decoded)
	}
}

func TestArtifactSharePublisherRevokesIntentWhenUploadFails(t *testing.T) {
	t.Parallel()
	revoked := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/device/artifact-shares/intents", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"share_id":"share-fail","upload_url":"/internal/v1/device/artifact-shares/share-fail/content","complete_url":"/internal/v1/device/artifact-shares/share-fail/complete","base_url":"https://share.example/s/share-fail","expires_at":"2026-08-08T00:00:00Z"}`)
	})
	mux.HandleFunc("PUT /internal/v1/device/artifact-shares/share-fail/content", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "failed", http.StatusBadGateway) })
	mux.HandleFunc("DELETE /internal/v1/device/artifact-shares/share-fail", func(w http.ResponseWriter, _ *http.Request) { revoked = true; w.WriteHeader(http.StatusNoContent) })
	server := httptest.NewServer(mux)
	defer server.Close()
	publisher := NewArtifactSharePublisher(server.Client())
	_, err := publisher.Publish(context.Background(), &Credentials{CloudURL: server.URL, DeviceToken: "token"}, ArtifactShareInput{
		ArtifactID: "artifact", Revision: 1, Title: "x", MediaType: "text/plain", Kind: "text", Content: []byte("x"), ExpiresIn: time.Hour,
	})
	if err == nil {
		t.Fatal("Publish unexpectedly succeeded")
	}
	if !revoked {
		t.Fatal("failed upload did not revoke the incomplete intent")
	}
}

func openTestEnvelope(t *testing.T, key, nonce, ciphertext, aad []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}
