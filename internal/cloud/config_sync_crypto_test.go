package cloud

import (
	"bytes"
	"testing"
)

func TestAccountSyncKeyWrapRoundTripAndWrongDevice(t *testing.T) {
	publicKey, privateKey, err := GenerateIdentityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	ask, err := GenerateAccountSyncKey()
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := WrapAccountSyncKey(publicKey, ask, 7)
	if err != nil {
		t.Fatal(err)
	}
	opened, keyGen, err := UnwrapAccountSyncKey(privateKey, *wrap)
	if err != nil {
		t.Fatal(err)
	}
	if keyGen != 7 || !bytes.Equal(opened, ask) {
		t.Fatalf("unwrap = key_gen %d key %x, want 7 %x", keyGen, opened, ask)
	}

	_, wrongPrivateKey, err := GenerateIdentityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UnwrapAccountSyncKey(wrongPrivateKey, *wrap); err == nil {
		t.Fatal("a different Desktop identity decrypted ASK")
	}
}

func TestAccountSyncKeyWrapRejectsMalformedIdentity(t *testing.T) {
	ask, err := GenerateAccountSyncKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WrapAccountSyncKey("not-base64", ask, 1); err == nil {
		t.Fatal("malformed public key accepted")
	}
	if _, err := WrapAccountSyncKey("", ask[:8], 1); err == nil {
		t.Fatal("short ASK accepted")
	}
}
