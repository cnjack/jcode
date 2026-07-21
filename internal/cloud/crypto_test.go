package cloud

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func testCipher(t *testing.T) *EnvelopeCipher {
	t.Helper()
	c, err := NewEnvelopeCipher(bytes.Repeat([]byte{0x42}, cekSize), 1)
	if err != nil {
		t.Fatalf("NewEnvelopeCipher: %v", err)
	}
	return c
}

func TestEnvelopeRoundTrip(t *testing.T) {
	c := testCipher(t)
	plaintext := json.RawMessage(`{"type":"agent_message","task_id":"s1","data":{"text":"hello"}}`)

	sealed, err := c.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if !IsEnvelope(sealed) {
		t.Fatalf("sealed payload not detected as envelope: %s", sealed)
	}
	var env Envelope
	if err := json.Unmarshal(sealed, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Enc != envelopeAlg || env.KeyGen != 1 || env.Nonce == "" || env.CT == "" {
		t.Fatalf("envelope fields = %+v", env)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil || len(nonce) != 12 {
		t.Fatalf("nonce must be 12 bytes base64, got %d bytes (err=%v)", len(nonce), err)
	}

	opened, err := c.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open = %s, want %s", opened, plaintext)
	}
}

func TestGreyDetectionPlaintextPassthrough(t *testing.T) {
	c := testCipher(t)
	// M3/M4-era plaintext payloads: no `enc` field, or `enc` present but not a
	// string — both must be read as-is.
	plaintexts := []json.RawMessage{
		json.RawMessage(`{"type":"agent_text","data":{"text":"hi"}}`),
		json.RawMessage(`{"enc":123,"foo":1}`), // non-string enc is NOT an envelope
		json.RawMessage(`"just a string"`),     // not even an object
		json.RawMessage(`[1,2,3]`),             // array
	}
	for _, p := range plaintexts {
		if IsEnvelope(p) {
			t.Errorf("IsEnvelope(%s) = true, want false", p)
		}
		got, wasEncrypted, err := c.OpenMaybe(p)
		if err != nil || wasEncrypted || !bytes.Equal(got, p) {
			t.Errorf("OpenMaybe(%s) = %s, %v, %v; want passthrough", p, got, wasEncrypted, err)
		}
	}
	// And a real envelope IS detected.
	sealed, err := c.Seal([]byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, wasEncrypted, err := c.OpenMaybe(sealed); err != nil || !wasEncrypted {
		t.Errorf("OpenMaybe(envelope) wasEncrypted=%v err=%v, want true, nil", wasEncrypted, err)
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	c1 := testCipher(t)
	c2, err := NewEnvelopeCipher(bytes.Repeat([]byte{0x99}, cekSize), 1)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c1.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c2.Open(sealed); err == nil {
		t.Fatal("Open with the wrong key succeeded, want decryption failure")
	}
}

func TestNewEnvelopeCipherRejectsBadKey(t *testing.T) {
	if _, err := NewEnvelopeCipher([]byte("short"), 1); err == nil {
		t.Fatal("16-byte CEK accepted, want error")
	}
	if _, err := NewEnvelopeCipher(bytes.Repeat([]byte{1}, cekSize), 0); err == nil {
		t.Fatal("key_gen=0 accepted, want error")
	}
}

// TestCrossEndVector pins the WebCrypto-interop vector shared with the console
// side (/jcode-cloud-relay/shared/test-vectors.json): fixed CEK + nonce must
// reproduce the exact ciphertext, and Open must recover the plaintext.
func TestCrossEndVector(t *testing.T) {
	const (
		cekB64   = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=" // bytes 0x00..0x1f
		nonceB64 = "amNvZGUtcmVsYXkh"                             // "jcode-relay!"
		ctB64    = "6TAl4JwmlZHU7dpwXh4NAzD72pWPn88OJKMWYJf1O1ztH6FrtOLSOHHb1s6UjkOtRs773hZQx7hi4bssDGxj6BdewORp7BsiBmijVdmnCNehQws="
	)
	plaintext := `{"type":"agent_message","task_id":"s1","data":{"text":"hello E2E"}}`

	cek, err := base64.StdEncoding.DecodeString(cekB64)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewEnvelopeCipher(cek, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Decrypt the pinned envelope.
	envelope := json.RawMessage(`{"enc":"aes-256-gcm","key_gen":1,"nonce":"` + nonceB64 + `","ct":"` + ctB64 + `"}`)
	opened, err := c.Open(envelope)
	if err != nil {
		t.Fatalf("Open(pinned vector): %v", err)
	}
	if string(opened) != plaintext {
		t.Fatalf("Open(pinned vector) = %s, want %s", opened, plaintext)
	}

	// Re-encrypt with the same key + nonce and require byte-identical ct.
	nonce, _ := base64.StdEncoding.DecodeString(nonceB64)
	ct := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	if base64.StdEncoding.EncodeToString(ct) != ctB64 {
		t.Fatalf("re-encryption ct = %s, want pinned %s", base64.StdEncoding.EncodeToString(ct), ctB64)
	}
}

// TestSharedVectorsFile reads the cross-implementation vectors shared with the
// console side (jcode-cloud-relay/shared/test-vectors.json: one Go-produced,
// one WebCrypto-produced) and requires every envelope to Open to exactly its
// recorded plaintext. Skipped when the checkout lacks the sibling directory.
func TestSharedVectorsFile(t *testing.T) {
	candidates := []string{
		"../../../jcode-cloud-relay/shared/test-vectors.json",
		"../../../../jcode-cloud-relay/shared/test-vectors.json",
	}
	var raw []byte
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			raw = b
			break
		}
	}
	if raw == nil {
		t.Skip("jcode-cloud-relay/shared/test-vectors.json not found next to the jcode repo")
	}
	var file struct {
		Vectors []struct {
			Origin    string `json:"origin"`
			CekB64    string `json:"cek_b64"`
			Plaintext string `json:"plaintext"`
			Envelope  struct {
				Enc    string `json:"enc"`
				KeyGen int    `json:"key_gen"`
				Nonce  string `json:"nonce"`
				Ct     string `json:"ct"`
			} `json:"envelope"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Vectors) == 0 {
		t.Fatal("shared vectors file is empty")
	}
	for _, v := range file.Vectors {
		t.Run(v.Origin, func(t *testing.T) {
			cek, err := base64.StdEncoding.DecodeString(v.CekB64)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewEnvelopeCipher(cek, v.Envelope.KeyGen)
			if err != nil {
				t.Fatal(err)
			}
			env, _ := json.Marshal(v.Envelope)
			opened, err := c.Open(env)
			if err != nil {
				t.Fatalf("Open(%s vector): %v", v.Origin, err)
			}
			if string(opened) != v.Plaintext {
				t.Fatalf("Open(%s vector) = %s, want %s", v.Origin, opened, v.Plaintext)
			}
		})
	}
}

// --- ECIES pairing wrap ---

// p256Requester generates a requester key pair the way the browser does:
// returns the SPKI DER base64 pubkey (what it sends) and the ECDH private key
// (what it keeps).
func p256Requester(t *testing.T) (pubB64 string, priv *ecdh.PrivateKey) {
	t.Helper()
	ecdsaPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&ecdsaPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	priv, err = ecdsaPriv.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der), priv
}

func TestWrapUnwrapRoundTrip(t *testing.T) {
	pubB64, priv := p256Requester(t)
	cek := bytes.Repeat([]byte{0x7a}, cekSize)

	wrap, err := WrapCEK(pubB64, cek, 3)
	if err != nil {
		t.Fatalf("WrapCEK: %v", err)
	}
	if wrap.EphemeralPubKey == "" || wrap.Nonce == "" || wrap.CT == "" {
		t.Fatalf("incomplete wrap: %+v", wrap)
	}
	// Ephemeral pubkey must itself be a valid P-256 SPKI.
	if _, err := parseP256SPKI(wrap.EphemeralPubKey); err != nil {
		t.Fatalf("ephemeral pubkey not parseable SPKI: %v", err)
	}

	gotCEK, gotGen, err := UnwrapCEK(wrap, priv)
	if err != nil {
		t.Fatalf("UnwrapCEK: %v", err)
	}
	if !bytes.Equal(gotCEK, cek) || gotGen != 3 {
		t.Fatalf("UnwrapCEK = (%x, %d), want (%x, 3)", gotCEK, gotGen, cek)
	}
}

func TestUnwrapWrongKeyFails(t *testing.T) {
	pubB64, _ := p256Requester(t)
	_, otherPriv := p256Requester(t)
	wrap, err := WrapCEK(pubB64, bytes.Repeat([]byte{0x7a}, cekSize), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UnwrapCEK(wrap, otherPriv); err == nil {
		t.Fatal("UnwrapCEK with the wrong private key succeeded, want failure")
	}
}

func TestWrapCEKRejectsBadPubKey(t *testing.T) {
	if _, err := WrapCEK("!!!not-base64!!!", bytes.Repeat([]byte{1}, cekSize), 1); err == nil {
		t.Fatal("WrapCEK accepted garbage pubkey, want error")
	}
	if _, err := WrapCEK(base64.StdEncoding.EncodeToString([]byte("not an spki")), bytes.Repeat([]byte{1}, cekSize), 1); err == nil {
		t.Fatal("WrapCEK accepted non-SPKI pubkey, want error")
	}
}

// --- recovery phrase ---

func TestPhraseRoundTrip(t *testing.T) {
	cek, err := GenerateCEK()
	if err != nil {
		t.Fatal(err)
	}
	phrase, err := CEKToPhrase(cek)
	if err != nil {
		t.Fatalf("CEKToPhrase: %v", err)
	}
	if words := strings.Fields(phrase); len(words) != 24 {
		t.Fatalf("phrase has %d words, want 24", len(words))
	}
	got, err := CEKFromPhrase(phrase)
	if err != nil {
		t.Fatalf("CEKFromPhrase: %v", err)
	}
	if !bytes.Equal(got, cek) {
		t.Fatal("CEKFromPhrase(CEKToPhrase(cek)) != cek")
	}
	// Extra whitespace / newlines are tolerated.
	got, err = CEKFromPhrase("  " + strings.ReplaceAll(phrase, " ", "  \n ") + "  ")
	if err != nil || !bytes.Equal(got, cek) {
		t.Fatalf("CEKFromPhrase with extra whitespace = %v, %v", got, err)
	}
}

func TestPhraseRejectsInvalid(t *testing.T) {
	if _, err := CEKFromPhrase("foo bar baz"); err == nil {
		t.Fatal("invalid phrase accepted")
	}
	if _, err := CEKFromPhrase(""); err == nil {
		t.Fatal("empty phrase accepted")
	}
	if _, err := CEKToPhrase([]byte("short")); err == nil {
		t.Fatal("CEKToPhrase accepted non-32-byte key")
	}
}

// --- CEK lifecycle (lazy generate + persist + cache) ---

func setupHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ResetCEKCache()
	t.Cleanup(ResetCEKCache)
}

func writeTestCreds(t *testing.T, creds *Credentials) {
	t.Helper()
	if err := SaveCredentials(creds); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
}

func TestEnsureCEKLazyGeneratePersistCache(t *testing.T) {
	setupHome(t)
	writeTestCreds(t, &Credentials{CloudURL: "https://c.example", DeviceID: "d1", DeviceToken: "tok", KeyGen: 1})

	c1, err := EnsureCEK()
	if err != nil {
		t.Fatalf("EnsureCEK: %v", err)
	}
	if c1.KeyGen() != 1 {
		t.Fatalf("key_gen = %d, want 1", c1.KeyGen())
	}
	// Persisted back to disk.
	creds, err := LoadCredentials()
	if err != nil || creds == nil {
		t.Fatalf("LoadCredentials = %+v, %v", creds, err)
	}
	if creds.CEK == "" {
		t.Fatal("CEK not persisted to cloud.json")
	}
	stored, err := base64.StdEncoding.DecodeString(creds.CEK)
	if err != nil || !bytes.Equal(stored, c1.CEK()) {
		t.Fatalf("stored CEK does not match the cached cipher")
	}
	// In-process cache: same pointer, and survives a cleared-file reload.
	c2, err := EnsureCEK()
	if err != nil || c2 != c1 {
		t.Fatalf("EnsureCEK cache = %p, %v; want %p", c2, err, c1)
	}
	// Fresh process (cache cleared) loads the same CEK from disk.
	ResetCEKCache()
	c3, err := EnsureCEK()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c3.CEK(), c1.CEK()) {
		t.Fatal("reloaded CEK differs from the persisted one")
	}
}

func TestEnsureCEKRequiresLogin(t *testing.T) {
	setupHome(t)
	if _, err := EnsureCEK(); err == nil {
		t.Fatal("EnsureCEK without credentials succeeded, want error")
	}
}

func TestEnsureCEKOldFileWithoutCEKStaysPlainUntilGenerated(t *testing.T) {
	setupHome(t)
	// Pre-M5 credentials file: no cek field at all.
	writeTestCreds(t, &Credentials{CloudURL: "https://c.example", DeviceID: "d1", DeviceToken: "tok", KeyGen: 1})
	creds, err := LoadCredentials()
	if err != nil || creds == nil {
		t.Fatal(err)
	}
	if creds.CEK != "" {
		t.Fatal("old credentials file must load with empty CEK (uninitialized)")
	}
}

func TestRotateCEK(t *testing.T) {
	setupHome(t)
	writeTestCreds(t, &Credentials{CloudURL: "https://c.example", DeviceID: "d1", DeviceToken: "tok", KeyGen: 1})
	old, err := EnsureCEK()
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := RotateCEK()
	if err != nil {
		t.Fatalf("RotateCEK: %v", err)
	}
	if rotated.KeyGen() != 2 {
		t.Fatalf("key_gen = %d, want 2", rotated.KeyGen())
	}
	if bytes.Equal(rotated.CEK(), old.CEK()) {
		t.Fatal("rotated CEK equals the old one")
	}
	creds, _ := LoadCredentials()
	if creds.KeyGen != 2 {
		t.Fatalf("persisted key_gen = %d, want 2", creds.KeyGen)
	}
	// The old key no longer opens new envelopes; the new one does.
	sealed, err := rotated.Seal([]byte("post-rotation"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Open(sealed); err == nil {
		t.Fatal("old key opened a post-rotation envelope")
	}
}

func TestRecoverCEK(t *testing.T) {
	setupHome(t)
	writeTestCreds(t, &Credentials{CloudURL: "https://c.example", DeviceID: "d1", DeviceToken: "tok", KeyGen: 1})
	cipher, err := EnsureCEK()
	if err != nil {
		t.Fatal(err)
	}
	phrase, err := CEKToPhrase(cipher.CEK())
	if err != nil {
		t.Fatal(err)
	}
	// Simulate total loss + recovery: rotate away, then recover from phrase.
	if _, err := RotateCEK(); err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverCEK(phrase)
	if err != nil {
		t.Fatalf("RecoverCEK: %v", err)
	}
	if !bytes.Equal(recovered.CEK(), cipher.CEK()) {
		t.Fatal("recovered CEK differs from the phrase's CEK")
	}
	if recovered.KeyGen() != 2 { // keeps the stored generation
		t.Fatalf("recovered key_gen = %d, want 2 (stored generation kept)", recovered.KeyGen())
	}
}
