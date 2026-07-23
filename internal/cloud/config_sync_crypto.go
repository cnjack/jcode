package cloud

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const accountSyncHKDFInfo = "jcode-account-sync-key"

// AccountSyncKeyWrap is the Desktop-to-Desktop X25519 envelope stored opaquely
// by Cloud. It is intentionally wire-compatible in shape with other key wraps
// while using a distinct curve and HKDF domain.
type AccountSyncKeyWrap struct {
	EphemeralPubKey string `json:"ephemeral_pubkey"`
	Nonce           string `json:"nonce"`
	CT              string `json:"ct"`
}

type accountSyncKeyPayload struct {
	ASK    string `json:"ask"`
	KeyGen int    `json:"key_gen"`
}

func GenerateAccountSyncKey() ([]byte, error) {
	key := make([]byte, cekSize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate account sync key: %w", err)
	}
	return key, nil
}

func deriveAccountSyncWrapKey(priv *ecdh.PrivateKey, pub *ecdh.PublicKey) ([]byte, error) {
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("account sync ECDH: %w", err)
	}
	hk := hkdf.New(sha256.New, shared, nil, []byte(accountSyncHKDFInfo))
	key := make([]byte, cekSize)
	if _, err := io.ReadFull(hk, key); err != nil {
		return nil, fmt.Errorf("derive account sync wrap key: %w", err)
	}
	return key, nil
}

func accountSyncWrapAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("account sync wrap AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("account sync wrap GCM: %w", err)
	}
	return aead, nil
}

// WrapAccountSyncKey seals ASK for the target Desktop's registered X25519 raw
// public key. Cloud receives only this envelope.
func WrapAccountSyncKey(targetPublicKey string, ask []byte, keyGen int) (*AccountSyncKeyWrap, error) {
	if len(ask) != cekSize || keyGen < 1 {
		return nil, fmt.Errorf("invalid account sync key material")
	}
	rawPub, err := base64.StdEncoding.DecodeString(targetPublicKey)
	if err != nil || len(rawPub) != 32 {
		return nil, fmt.Errorf("target public key must be a 32-byte X25519 key")
	}
	target, err := ecdh.X25519().NewPublicKey(rawPub)
	if err != nil {
		return nil, fmt.Errorf("parse target X25519 public key: %w", err)
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate account sync ephemeral key: %w", err)
	}
	wrapKey, err := deriveAccountSyncWrapKey(ephemeral, target)
	if err != nil {
		return nil, err
	}
	aead, err := accountSyncWrapAEAD(wrapKey)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(accountSyncKeyPayload{
		ASK: base64.StdEncoding.EncodeToString(ask), KeyGen: keyGen,
	})
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate account sync wrap nonce: %w", err)
	}
	return &AccountSyncKeyWrap{
		EphemeralPubKey: base64.StdEncoding.EncodeToString(ephemeral.PublicKey().Bytes()),
		Nonce:           base64.StdEncoding.EncodeToString(nonce),
		CT:              base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plain, nil)),
	}, nil
}

// UnwrapAccountSyncKey opens a Cloud-delivered wrap with this Desktop's
// X25519 private identity key.
func UnwrapAccountSyncKey(privateKey string, wrap AccountSyncKeyWrap) ([]byte, int, error) {
	rawPriv, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil || len(rawPriv) != 32 {
		return nil, 0, fmt.Errorf("private identity key must be a 32-byte X25519 key")
	}
	priv, err := ecdh.X25519().NewPrivateKey(rawPriv)
	if err != nil {
		return nil, 0, fmt.Errorf("parse private X25519 key: %w", err)
	}
	rawEphemeral, err := base64.StdEncoding.DecodeString(wrap.EphemeralPubKey)
	if err != nil || len(rawEphemeral) != 32 {
		return nil, 0, fmt.Errorf("ephemeral public key must be a 32-byte X25519 key")
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(rawEphemeral)
	if err != nil {
		return nil, 0, fmt.Errorf("parse ephemeral X25519 key: %w", err)
	}
	wrapKey, err := deriveAccountSyncWrapKey(priv, ephemeral)
	if err != nil {
		return nil, 0, err
	}
	aead, err := accountSyncWrapAEAD(wrapKey)
	if err != nil {
		return nil, 0, err
	}
	nonce, err := base64.StdEncoding.DecodeString(wrap.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, 0, fmt.Errorf("account sync wrap nonce is invalid")
	}
	ct, err := base64.StdEncoding.DecodeString(wrap.CT)
	if err != nil {
		return nil, 0, fmt.Errorf("account sync wrap ciphertext is invalid")
	}
	plain, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("open account sync key wrap: %w", err)
	}
	var payload accountSyncKeyPayload
	dec := json.NewDecoder(strings.NewReader(string(plain)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return nil, 0, fmt.Errorf("parse account sync key payload: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, 0, fmt.Errorf("parse account sync key payload: trailing JSON")
	}
	ask, err := base64.StdEncoding.DecodeString(payload.ASK)
	if err != nil || len(ask) != cekSize || payload.KeyGen < 1 {
		return nil, 0, fmt.Errorf("account sync key payload is invalid")
	}
	return ask, payload.KeyGen, nil
}

func accountSyncCipherFromCredentials(creds *Credentials) (*EnvelopeCipher, error) {
	if creds == nil || creds.ASK == "" || creds.ASKKeyGen < 1 {
		return nil, errors.New("account sync key is not available")
	}
	ask, err := base64.StdEncoding.DecodeString(creds.ASK)
	if err != nil {
		return nil, fmt.Errorf("decode stored account sync key: %w", err)
	}
	return NewEnvelopeCipher(ask, creds.ASKKeyGen)
}

func saveAccountSyncKey(creds *Credentials, ask []byte, keyGen int) error {
	if creds == nil || len(ask) != cekSize || keyGen < 1 {
		return errors.New("invalid account sync key")
	}
	creds.ASK = base64.StdEncoding.EncodeToString(ask)
	creds.ASKKeyGen = keyGen
	return SaveCredentials(creds)
}
