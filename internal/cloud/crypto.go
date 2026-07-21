// crypto.go implements the M5 E2E encryption layer (定稿契约，勿改 wire 格式):
//
//   - CEK: one 32-byte AES-256-GCM key per account, stored base64 in
//     ~/.jcode/cloud.json (`cek`, generation in `key_gen`). Lazily generated
//     on first need (connector start / `jcode cloud` commands), atomically
//     written back, cached in-process.
//   - Envelope: {"enc":"aes-256-gcm","key_gen":N,"nonce":"<b64 12B>","ct":"<b64>"}.
//     Sealed fields: uplink events/ephemeral payload, sessions upsert meta,
//     ack result; downlink command payload. Grey rule: a JSON object payload
//     with a string `enc` field is treated as an envelope and decrypted;
//     anything else is read as plaintext (M3/M4 compatibility).
//   - Pairing wrap (ECIES/P-256): ephemeral P-256 key pair →
//     ECDH(ephemeral, requester pubkey) → HKDF-SHA256(shared, salt=nil,
//     info="jcode-device-cek") → 32B wrap key → AES-256-GCM over
//     {"cek":"<b64>","key_gen":N}.
//   - Recovery: the CEK doubles as 256 bits of BIP39 entropy → 24-word phrase.
package cloud

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync"

	"golang.org/x/crypto/hkdf"

	"github.com/tyler-smith/go-bip39"
)

// envelopeAlg is the `enc` marker of a sealed payload.
const envelopeAlg = "aes-256-gcm"

// cekSize is the CEK length in bytes (AES-256).
const cekSize = 32

// hkdfInfo is the HKDF-SHA256 info string for pairing wrap-key derivation.
const hkdfInfo = "jcode-device-cek"

// Envelope is the sealed-payload wire format.
type Envelope struct {
	Enc    string `json:"enc"`
	KeyGen int    `json:"key_gen"`
	Nonce  string `json:"nonce"` // base64, 12 bytes
	CT     string `json:"ct"`    // base64 ciphertext
}

// EnvelopeCipher seals/opens payloads with one CEK generation.
type EnvelopeCipher struct {
	key    []byte
	keyGen int
	aead   cipher.AEAD
}

// NewEnvelopeCipher builds a cipher from a raw 32-byte CEK and its generation.
func NewEnvelopeCipher(cek []byte, keyGen int) (*EnvelopeCipher, error) {
	if len(cek) != cekSize {
		return nil, fmt.Errorf("CEK must be %d bytes, got %d", cekSize, len(cek))
	}
	if keyGen < 1 {
		return nil, fmt.Errorf("key_gen must be >= 1, got %d", keyGen)
	}
	aead, err := newAEAD(cek)
	if err != nil {
		return nil, err
	}
	key := make([]byte, cekSize)
	copy(key, cek)
	return &EnvelopeCipher{key: key, keyGen: keyGen, aead: aead}, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return aead, nil
}

// KeyGen reports the CEK generation this cipher seals with.
func (e *EnvelopeCipher) KeyGen() int { return e.keyGen }

// CEK returns a copy of the raw CEK bytes.
func (e *EnvelopeCipher) CEK() []byte {
	out := make([]byte, len(e.key))
	copy(out, e.key)
	return out
}

// Seal encrypts plaintext into the envelope wire format (as JSON).
func (e *EnvelopeCipher) Seal(plaintext []byte) (json.RawMessage, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("seal: nonce: %w", err)
	}
	env := Envelope{
		Enc:    envelopeAlg,
		KeyGen: e.keyGen,
		Nonce:  base64.StdEncoding.EncodeToString(nonce),
		CT:     base64.StdEncoding.EncodeToString(e.aead.Seal(nil, nonce, plaintext, nil)),
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("seal: marshal envelope: %w", err)
	}
	return json.RawMessage(data), nil
}

// IsEnvelope applies the grey-scale detection rule: raw is a JSON object with
// a string `enc` field.
func IsEnvelope(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		Enc string `json:"enc"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Enc != ""
}

// Open decrypts an envelope payload. The caller must have applied IsEnvelope
// first (or use OpenMaybe); passing plaintext here is an error.
func (e *EnvelopeCipher) Open(raw json.RawMessage) ([]byte, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("open: parse envelope: %w", err)
	}
	if env.Enc != envelopeAlg {
		return nil, fmt.Errorf("open: unsupported enc %q", env.Enc)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("open: nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(env.CT)
	if err != nil {
		return nil, fmt.Errorf("open: ct: %w", err)
	}
	plain, err := e.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("open: decrypt failed (wrong key or corrupted data): %w", err)
	}
	return plain, nil
}

// OpenMaybe decrypts raw when it is an envelope, otherwise returns it
// unchanged (plaintext grey-scale passthrough). The boolean reports whether
// decryption happened.
func (e *EnvelopeCipher) OpenMaybe(raw json.RawMessage) ([]byte, bool, error) {
	if !IsEnvelope(raw) {
		return raw, false, nil
	}
	plain, err := e.Open(raw)
	if err != nil {
		return nil, true, err
	}
	return plain, true, nil
}

// --- CEK lifecycle (lazy generate + atomic write-back + in-process cache) ---

var cekCache struct {
	mu     sync.Mutex
	cipher *EnvelopeCipher
}

// ResetCEKCache clears the in-process CEK cache. Called on logout and by
// tests that swap the credentials file.
func ResetCEKCache() {
	cekCache.mu.Lock()
	defer cekCache.mu.Unlock()
	cekCache.cipher = nil
}

// GenerateCEK returns a fresh random 32-byte CEK.
func GenerateCEK() ([]byte, error) {
	cek := make([]byte, cekSize)
	if _, err := io.ReadFull(rand.Reader, cek); err != nil {
		return nil, fmt.Errorf("generate CEK: %w", err)
	}
	return cek, nil
}

// EnsureCEK returns the account CEK cipher, lazily generating and persisting
// the CEK on first use (atomic write-back to ~/.jcode/cloud.json). The cipher
// is cached in-process afterwards. Requires the device to be logged in.
func EnsureCEK() (*EnvelopeCipher, error) {
	cekCache.mu.Lock()
	defer cekCache.mu.Unlock()
	if cekCache.cipher != nil {
		return cekCache.cipher, nil
	}
	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	if creds == nil {
		return nil, errors.New("not logged in: run `jcode login` first")
	}
	cipher, changed, err := ensureCEKLocked(creds)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := SaveCredentials(creds); err != nil {
			return nil, fmt.Errorf("persist generated CEK: %w", err)
		}
	}
	cekCache.cipher = cipher
	return cipher, nil
}

// ensureCEKLocked builds the cipher from creds, generating a CEK into creds
// when missing. changed reports whether creds was mutated (caller persists).
func ensureCEKLocked(creds *Credentials) (cipher *EnvelopeCipher, changed bool, err error) {
	if creds.KeyGen < 1 {
		creds.KeyGen = 1
		changed = true
	}
	if creds.CEK == "" {
		cek, err := GenerateCEK()
		if err != nil {
			return nil, false, err
		}
		creds.CEK = base64.StdEncoding.EncodeToString(cek)
		return mustCipher(cek, creds.KeyGen), true, nil
	}
	cek, err := base64.StdEncoding.DecodeString(creds.CEK)
	if err != nil {
		return nil, false, fmt.Errorf("decode stored CEK: %w", err)
	}
	c, err := NewEnvelopeCipher(cek, creds.KeyGen)
	if err != nil {
		return nil, false, err
	}
	return c, changed, nil
}

func mustCipher(cek []byte, keyGen int) *EnvelopeCipher {
	c, err := NewEnvelopeCipher(cek, keyGen)
	if err != nil { // cek is freshly generated with the right size
		panic(err)
	}
	return c
}

// RotateCEK generates a new CEK at generation key_gen+1, persists it, and
// updates the in-process cache. Already-paired clients keep the old CEK and
// must re-pair to read new content.
func RotateCEK() (*EnvelopeCipher, error) {
	cekCache.mu.Lock()
	defer cekCache.mu.Unlock()
	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	if creds == nil {
		return nil, errors.New("not logged in: run `jcode login` first")
	}
	keyGen := creds.KeyGen + 1
	if keyGen < 1 {
		keyGen = 1
	}
	cek, err := GenerateCEK()
	if err != nil {
		return nil, err
	}
	creds.CEK = base64.StdEncoding.EncodeToString(cek)
	creds.KeyGen = keyGen
	if err := SaveCredentials(creds); err != nil {
		return nil, fmt.Errorf("persist rotated CEK: %w", err)
	}
	cipher := mustCipher(cek, keyGen)
	cekCache.cipher = cipher
	return cipher, nil
}

// RecoverCEK rebuilds the CEK from a 24-word recovery phrase and persists it
// (keeping the stored key_gen, default 1), overwriting any existing CEK. The
// caller is responsible for warning and confirmation before calling.
func RecoverCEK(phrase string) (*EnvelopeCipher, error) {
	cek, err := CEKFromPhrase(phrase)
	if err != nil {
		return nil, err
	}
	cekCache.mu.Lock()
	defer cekCache.mu.Unlock()
	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	if creds == nil {
		return nil, errors.New("not logged in: run `jcode login` first")
	}
	if creds.KeyGen < 1 {
		creds.KeyGen = 1
	}
	creds.CEK = base64.StdEncoding.EncodeToString(cek)
	if err := SaveCredentials(creds); err != nil {
		return nil, fmt.Errorf("persist recovered CEK: %w", err)
	}
	cipher := mustCipher(cek, creds.KeyGen)
	cekCache.cipher = cipher
	return cipher, nil
}

// --- recovery phrase (BIP39: the 32-byte CEK is the 256-bit entropy) ---

// CEKToPhrase encodes a 32-byte CEK as its 24-word BIP39 recovery phrase.
func CEKToPhrase(cek []byte) (string, error) {
	if len(cek) != cekSize {
		return "", fmt.Errorf("CEK must be %d bytes for a 24-word phrase, got %d", cekSize, len(cek))
	}
	phrase, err := bip39.NewMnemonic(cek)
	if err != nil {
		return "", fmt.Errorf("encode recovery phrase: %w", err)
	}
	return phrase, nil
}

// CEKFromPhrase decodes a 24-word BIP39 recovery phrase back into the CEK.
func CEKFromPhrase(phrase string) ([]byte, error) {
	phrase = strings.Join(strings.Fields(strings.TrimSpace(phrase)), " ")
	if !bip39.IsMnemonicValid(phrase) {
		return nil, fmt.Errorf("invalid recovery phrase (want 24 BIP39 words)")
	}
	cek, err := bip39.EntropyFromMnemonic(phrase)
	if err != nil {
		return nil, fmt.Errorf("decode recovery phrase: %w", err)
	}
	if len(cek) != cekSize {
		return nil, fmt.Errorf("recovery phrase yields %d bytes, want a %d-byte CEK (24 words)", len(cek), cekSize)
	}
	return cek, nil
}

// --- pairing wrap (ECIES / P-256) ---

// CEKWrap is the ECIES-wrapped CEK sent back to an approved pairing requester.
type CEKWrap struct {
	EphemeralPubKey string `json:"ephemeral_pubkey"` // base64 SPKI DER, P-256
	Nonce           string `json:"nonce"`            // base64, 12 bytes
	CT              string `json:"ct"`               // base64 ciphertext
}

// wrappedCEKPayload is the plaintext inside a CEKWrap.
type wrappedCEKPayload struct {
	CEK    string `json:"cek"`
	KeyGen int    `json:"key_gen"`
}

// parseP256SPKI decodes a base64 SPKI DER P-256 public key into an ECDH key.
func parseP256SPKI(b64 string) (*ecdh.PublicKey, error) {
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("pubkey base64: %w", err)
	}
	pubAny, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("pubkey SPKI: %w", err)
	}
	ecdsaPub, ok := pubAny.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("pubkey is %T, want an ECDSA P-256 key", pubAny)
	}
	if ecdsaPub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("pubkey curve is not P-256")
	}
	ecdhPub, err := ecdsaPub.ECDH()
	if err != nil {
		return nil, fmt.Errorf("pubkey ECDH: %w", err)
	}
	return ecdhPub, nil
}

// ecdhPubToSPKI converts an ECDH P-256 public key to SPKI DER bytes. The
// ecdh key's Bytes() is the SEC 1 uncompressed point (0x04 || X || Y).
func ecdhPubToSPKI(pub *ecdh.PublicKey) ([]byte, error) {
	raw := pub.Bytes()
	if len(raw) != 65 || raw[0] != 0x04 {
		return nil, fmt.Errorf("unexpected P-256 point encoding (%d bytes)", len(raw))
	}
	x := new(big.Int).SetBytes(raw[1:33])
	y := new(big.Int).SetBytes(raw[33:65])
	return x509.MarshalPKIXPublicKey(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y})
}

// deriveWrapKey runs ECDH + HKDF-SHA256(shared, salt=nil, info="jcode-device-cek").
func deriveWrapKey(priv *ecdh.PrivateKey, pub *ecdh.PublicKey) ([]byte, error) {
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("ECDH: %w", err)
	}
	hk := hkdf.New(sha256.New, shared, nil, []byte(hkdfInfo))
	key := make([]byte, cekSize)
	if _, err := io.ReadFull(hk, key); err != nil {
		return nil, fmt.Errorf("HKDF: %w", err)
	}
	return key, nil
}

// WrapCEK encrypts the CEK for a pairing requester: requesterPubKeyB64 is the
// requester's P-256 public key (base64 SPKI DER, as generated by WebCrypto).
func WrapCEK(requesterPubKeyB64 string, cek []byte, keyGen int) (*CEKWrap, error) {
	if len(cek) != cekSize {
		return nil, fmt.Errorf("CEK must be %d bytes, got %d", cekSize, len(cek))
	}
	requesterPub, err := parseP256SPKI(requesterPubKeyB64)
	if err != nil {
		return nil, fmt.Errorf("requester pubkey: %w", err)
	}
	eph, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ephemeral key: %w", err)
	}
	wrapKey, err := deriveWrapKey(eph, requesterPub)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(wrapKey)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(wrappedCEKPayload{
		CEK:    base64.StdEncoding.EncodeToString(cek),
		KeyGen: keyGen,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal wrap payload: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("wrap nonce: %w", err)
	}
	ephDER, err := ecdhPubToSPKI(eph.PublicKey())
	if err != nil {
		return nil, err
	}
	return &CEKWrap{
		EphemeralPubKey: base64.StdEncoding.EncodeToString(ephDER),
		Nonce:           base64.StdEncoding.EncodeToString(nonce),
		CT:              base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plain, nil)),
	}, nil
}

// UnwrapCEK reverses WrapCEK with the requester's P-256 private key. The
// device side never calls this in production — it exists for the requester
// side and for round-trip tests.
func UnwrapCEK(wrap *CEKWrap, requesterPriv *ecdh.PrivateKey) (cek []byte, keyGen int, err error) {
	ephPub, err := parseP256SPKI(wrap.EphemeralPubKey)
	if err != nil {
		return nil, 0, fmt.Errorf("ephemeral pubkey: %w", err)
	}
	wrapKey, err := deriveWrapKey(requesterPriv, ephPub)
	if err != nil {
		return nil, 0, err
	}
	aead, err := newAEAD(wrapKey)
	if err != nil {
		return nil, 0, err
	}
	nonce, err := base64.StdEncoding.DecodeString(wrap.Nonce)
	if err != nil {
		return nil, 0, fmt.Errorf("wrap nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(wrap.CT)
	if err != nil {
		return nil, 0, fmt.Errorf("wrap ct: %w", err)
	}
	plain, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("unwrap failed (wrong key or corrupted wrap): %w", err)
	}
	var payload wrappedCEKPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, 0, fmt.Errorf("unwrap payload: %w", err)
	}
	cek, err = base64.StdEncoding.DecodeString(payload.CEK)
	if err != nil {
		return nil, 0, fmt.Errorf("unwrap cek: %w", err)
	}
	return cek, payload.KeyGen, nil
}
