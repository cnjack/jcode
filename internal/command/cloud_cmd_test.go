package command

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/cloud"
)

// pairingTestServer implements the device-token pairing endpoints plus
// heartbeat, recording respond calls for assertions.
type pairingTestServer struct {
	srv *httptest.Server

	requesterPriv *ecdh.PrivateKey
	requesterPub  string // base64 SPKI

	responds []respondRecord
}

type respondRecord struct {
	id      string
	approve bool
	wrap    *cloud.CEKWrap
}

func newPairingTestServer(t *testing.T) *pairingTestServer {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	ecdhPriv, err := priv.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	p := &pairingTestServer{
		requesterPriv: ecdhPriv,
		requesterPub:  base64.StdEncoding.EncodeToString(der),
	}
	pairing := cloud.Pairing{
		ID:        "pair-1",
		Label:     "chrome on mac",
		PubKey:    p.requesterPub,
		Status:    "pending",
		CreatedAt: "2026-07-20T10:00:00Z",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/device/pairings", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"pairings": []cloud.Pairing{pairing}})
	})
	mux.HandleFunc("GET /internal/v1/device/pairings/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(pairing)
	})
	mux.HandleFunc("POST /internal/v1/device/pairings/{id}/respond", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Approve bool           `json:"approve"`
			Wrap    *cloud.CEKWrap `json:"wrap"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.responds = append(p.responds, respondRecord{id: r.PathValue("id"), approve: body.Approve, wrap: body.Wrap})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("POST /internal/v1/device/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

// setupCloudHome points the credentials file at a temp HOME and stores device
// credentials for the test server (no CEK unless withCEK).
func setupCloudHome(t *testing.T, cloudURL string, withCEK bool) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cloud.ResetCEKCache()
	t.Cleanup(cloud.ResetCEKCache)
	creds := &cloud.Credentials{
		CloudURL:    cloudURL,
		DeviceID:    "dev-1",
		DeviceToken: "tok",
		DeviceName:  "test-device",
		KeyGen:      1,
	}
	if withCEK {
		cek := bytes.Repeat([]byte{0x11}, 32)
		creds.CEK = base64.StdEncoding.EncodeToString(cek)
	}
	if err := cloud.SaveCredentials(creds); err != nil {
		t.Fatal(err)
	}
}

func TestRunCloudPairings(t *testing.T) {
	pt := newPairingTestServer(t)
	setupCloudHome(t, pt.srv.URL, false)

	var out bytes.Buffer
	if err := runCloudPairings(context.Background(), &out); err != nil {
		t.Fatalf("runCloudPairings: %v", err)
	}
	if !strings.Contains(out.String(), "pair-1") || !strings.Contains(out.String(), "chrome on mac") {
		t.Fatalf("output = %q, want the pending pairing listed", out.String())
	}
}

func TestRunCloudApproveWrapsCEK(t *testing.T) {
	pt := newPairingTestServer(t)
	setupCloudHome(t, pt.srv.URL, false) // no CEK yet: approve must lazily generate it

	var out bytes.Buffer
	if err := runCloudApprove(context.Background(), &out, "pair-1"); err != nil {
		t.Fatalf("runCloudApprove: %v", err)
	}
	if !strings.Contains(out.String(), "chrome on mac") {
		t.Fatalf("output = %q, want the approved label", out.String())
	}
	if len(pt.responds) != 1 || !pt.responds[0].approve || pt.responds[0].wrap == nil {
		t.Fatalf("responds = %+v, want one approval with a wrap", pt.responds)
	}
	// The requester can unwrap the CEK; it must match what the device persisted.
	gotCEK, gotGen, err := cloud.UnwrapCEK(pt.responds[0].wrap, pt.requesterPriv)
	if err != nil {
		t.Fatalf("UnwrapCEK: %v", err)
	}
	creds, err := cloud.LoadCredentials()
	if err != nil || creds == nil {
		t.Fatal("credentials missing after approve")
	}
	stored, _ := base64.StdEncoding.DecodeString(creds.CEK)
	if !bytes.Equal(gotCEK, stored) || gotGen != creds.KeyGen {
		t.Fatalf("wrapped CEK (gen %d) does not match stored credentials (gen %d)", gotGen, creds.KeyGen)
	}
}

func TestRunCloudDeny(t *testing.T) {
	pt := newPairingTestServer(t)
	setupCloudHome(t, pt.srv.URL, false)

	var out bytes.Buffer
	if err := runCloudDeny(context.Background(), &out, "pair-1"); err != nil {
		t.Fatalf("runCloudDeny: %v", err)
	}
	if len(pt.responds) != 1 || pt.responds[0].approve || pt.responds[0].wrap != nil {
		t.Fatalf("responds = %+v, want one denial without wrap", pt.responds)
	}
	if !strings.Contains(out.String(), "Denied pairing pair-1") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunCloudStatus(t *testing.T) {
	pt := newPairingTestServer(t)
	setupCloudHome(t, pt.srv.URL, true)

	var out bytes.Buffer
	if err := runCloudStatus(context.Background(), &out); err != nil {
		t.Fatalf("runCloudStatus: %v", err)
	}
	s := out.String()
	for _, want := range []string{pt.srv.URL, "dev-1", "key gen:     1", "initialized", "online:      yes"} {
		if !strings.Contains(s, want) {
			t.Errorf("status output missing %q:\n%s", want, s)
		}
	}
}

func TestRunCloudKeyShowPhrase(t *testing.T) {
	pt := newPairingTestServer(t)
	setupCloudHome(t, pt.srv.URL, false)

	// Declining the confirmation prints nothing sensitive.
	var out bytes.Buffer
	if err := runCloudKeyShowPhrase(&out, strings.NewReader("no\n")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Recovery phrase (") {
		t.Fatalf("phrase revealed without confirmation:\n%s", out.String())
	}

	// Confirming reveals exactly 24 words matching the persisted CEK.
	out.Reset()
	if err := runCloudKeyShowPhrase(&out, strings.NewReader("yes\n")); err != nil {
		t.Fatal(err)
	}
	phrase := extractPhrase(t, out.String())
	cek, err := cloud.CEKFromPhrase(phrase)
	if err != nil {
		t.Fatalf("printed phrase does not decode: %v", err)
	}
	creds, _ := cloud.LoadCredentials()
	stored, _ := base64.StdEncoding.DecodeString(creds.CEK)
	if !bytes.Equal(cek, stored) {
		t.Fatal("printed phrase does not match the stored CEK")
	}
}

// extractPhrase pulls the indented 24-word line out of show-phrase output.
func extractPhrase(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if words := strings.Fields(line); len(words) == 24 {
			return strings.Join(words, " ")
		}
	}
	t.Fatalf("no 24-word phrase line in output:\n%s", output)
	return ""
}

func TestRunCloudKeyRecover(t *testing.T) {
	pt := newPairingTestServer(t)
	setupCloudHome(t, pt.srv.URL, false)

	// Generate a phrase from a known CEK (as if exported on another device).
	known, err := cloud.GenerateCEK()
	if err != nil {
		t.Fatal(err)
	}
	phrase, err := cloud.CEKToPhrase(known)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runCloudKeyRecover(&out, strings.NewReader(phrase+"\n")); err != nil {
		t.Fatalf("runCloudKeyRecover: %v", err)
	}
	creds, _ := cloud.LoadCredentials()
	stored, _ := base64.StdEncoding.DecodeString(creds.CEK)
	if !bytes.Equal(stored, known) {
		t.Fatal("recovered CEK does not match the phrase")
	}

	// Recovering over an existing CEK requires confirmation; declining keeps it.
	out.Reset()
	other, _ := cloud.GenerateCEK()
	otherPhrase, _ := cloud.CEKToPhrase(other)
	if err := runCloudKeyRecover(&out, strings.NewReader(otherPhrase+"\nno\n")); err != nil {
		t.Fatal(err)
	}
	creds, _ = cloud.LoadCredentials()
	stored, _ = base64.StdEncoding.DecodeString(creds.CEK)
	if !bytes.Equal(stored, known) {
		t.Fatal("CEK overwritten despite declined confirmation")
	}

	// An invalid phrase is rejected before any overwrite.
	out.Reset()
	if err := runCloudKeyRecover(&out, strings.NewReader("foo bar baz\n")); err == nil {
		t.Fatal("invalid phrase accepted")
	}
}

func TestRunCloudRotateKey(t *testing.T) {
	pt := newPairingTestServer(t)
	setupCloudHome(t, pt.srv.URL, true) // CEK present at key_gen 1

	var out bytes.Buffer
	if err := runCloudRotateKey(&out, strings.NewReader("yes\n")); err != nil {
		t.Fatalf("runCloudRotateKey: %v", err)
	}
	if !strings.Contains(out.String(), "key_gen=2") {
		t.Fatalf("output = %q, want key_gen=2 notice", out.String())
	}
	creds, _ := cloud.LoadCredentials()
	if creds.KeyGen != 2 || creds.CEK == "" {
		t.Fatalf("credentials after rotate = %+v", creds)
	}

	// Declining keeps key_gen 1… but we already rotated; use a fresh home.
	setupCloudHome(t, pt.srv.URL, true)
	out.Reset()
	if err := runCloudRotateKey(&out, strings.NewReader("no\n")); err != nil {
		t.Fatal(err)
	}
	creds, _ = cloud.LoadCredentials()
	if creds.KeyGen != 1 {
		t.Fatalf("key_gen = %d after declined rotation, want 1", creds.KeyGen)
	}
}

func TestCloudCommandsRequireLogin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cloud.ResetCEKCache()
	t.Cleanup(cloud.ResetCEKCache)

	var out bytes.Buffer
	if err := runCloudPairings(context.Background(), &out); err == nil {
		t.Error("pairings without login succeeded, want error")
	}
	if err := runCloudApprove(context.Background(), &out, "x"); err == nil {
		t.Error("approve without login succeeded, want error")
	}
	if err := runCloudStatus(context.Background(), &out); err == nil {
		t.Error("status without login succeeded, want error")
	}
}
