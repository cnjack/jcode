// crypto_wiring_test.go covers the M5 encryption wiring in the connector:
// uplink sealing (durable events, ephemeral events, sessions meta, ack
// result) and downlink opening (command payloads), each in both the
// encrypted and the plaintext grey path.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

func wiringCipher(t *testing.T) *EnvelopeCipher {
	t.Helper()
	c, err := NewEnvelopeCipher(bytes.Repeat([]byte{0x42}, cekSize), 1)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// withCipher injects a cipher into a test connector (bypassing the lazy
// EnsureCEK, which would touch the real credentials file).
func withCipher(conn *Connector, cipher *EnvelopeCipher) *Connector {
	conn.cipher = cipher
	return conn
}

func TestDurableEventSealedWhenCipherActive(t *testing.T) {
	mock := newMockCloud()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	conn := withCipher(newTestConnector(t, srv.URL, "http://127.0.0.1:1"), wiringCipher(t))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batcher := newEventBatcher(conn)
	go batcher.run(ctx)

	msg := `{"type":"user_message","task_id":"s1","data":{"text":"secret hello"}}`
	conn.handleWSEvent(ctx, batcher, []byte(msg))

	waitFor(t, func() bool { return len(mock.allEvents()) == 1 }, "sealed durable upload")
	got := mock.allEvents()[0]
	if !IsEnvelope(got.Payload) {
		t.Fatalf("durable payload is not an envelope: %s", got.Payload)
	}
	plain, err := conn.cipher.Open(got.Payload)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plain) != msg {
		t.Fatalf("decrypted payload = %s, want %s", plain, msg)
	}
}

func TestDurableEventPlaintextWithoutCipher(t *testing.T) {
	mock := newMockCloud()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	conn := newTestConnector(t, srv.URL, "http://127.0.0.1:1") // no cipher: grey path

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batcher := newEventBatcher(conn)
	go batcher.run(ctx)

	msg := `{"type":"user_message","task_id":"s1","data":{"text":"plain hello"}}`
	conn.handleWSEvent(ctx, batcher, []byte(msg))

	waitFor(t, func() bool { return len(mock.allEvents()) == 1 }, "plaintext durable upload")
	got := mock.allEvents()[0]
	if IsEnvelope(got.Payload) {
		t.Fatalf("plaintext-path payload unexpectedly sealed: %s", got.Payload)
	}
	if string(got.Payload) != msg {
		t.Fatalf("payload = %s, want %s", got.Payload, msg)
	}
}

func TestEphemeralSealedWhenCipherActive(t *testing.T) {
	mock := newMockCloud()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	conn := withCipher(newTestConnector(t, srv.URL, "http://127.0.0.1:1"), wiringCipher(t))

	ctx := context.Background()
	batcher := newEventBatcher(conn)
	msg := `{"type":"agent_text","task_id":"s1","data":{"text":"token"}}`
	conn.handleWSEvent(ctx, batcher, []byte(msg))

	waitFor(t, func() bool { return mock.ephemeralCount() == 1 }, "ephemeral upload")
	mock.mu.Lock()
	rec := mock.ephemeral[0]
	mock.mu.Unlock()
	if !IsEnvelope(rec.payload) {
		t.Fatalf("ephemeral payload is not an envelope: %s", rec.payload)
	}
	plain, err := conn.cipher.Open(rec.payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != msg {
		t.Fatalf("decrypted ephemeral = %s, want %s", plain, msg)
	}
}

func TestSessionsMetaSealedWhenCipherActive(t *testing.T) {
	conn := withCipher(newTestConnector(t, "http://127.0.0.1:1", "http://127.0.0.1:1"), wiringCipher(t))
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{
			"proj": {{UUID: "s1", Title: "secret title", Status: "idle", StartTime: "2026-07-24T01:30:00+08:00"}},
		}, nil
	}
	upserts, err := conn.collectSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(upserts))
	}
	if !IsEnvelope(upserts[0].Meta) {
		t.Fatalf("meta is not an envelope: %s", upserts[0].Meta)
	}
	if got := upserts[0].LastActivityAt; got != "2026-07-23T17:30:00Z" {
		t.Fatalf("last_activity_at = %q, want plaintext UTC timestamp", got)
	}
	plain, err := conn.cipher.Open(upserts[0].Meta)
	if err != nil {
		t.Fatal(err)
	}
	var meta session.SessionMeta
	if err := json.Unmarshal(plain, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.UUID != "s1" || meta.Title != "secret title" {
		t.Fatalf("decrypted meta = %+v", meta)
	}
}

func TestSessionsMetaPlaintextWithoutCipher(t *testing.T) {
	conn := newTestConnector(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{"proj": {{UUID: "s1"}}}, nil
	}
	upserts, err := conn.collectSessions()
	if err != nil {
		t.Fatal(err)
	}
	if IsEnvelope(upserts[0].Meta) {
		t.Fatalf("plaintext-path meta unexpectedly sealed: %s", upserts[0].Meta)
	}
}

func TestAckResultSealedWhenCipherActive(t *testing.T) {
	local, localSrv := newFakeLocal(t)
	mock := newMockCloud()
	cloudSrv := httptest.NewServer(mock.handler())
	defer cloudSrv.Close()
	conn := withCipher(newTestConnector(t, cloudSrv.URL, localSrv.URL), wiringCipher(t))

	sealed, err := conn.cipher.Seal(mustPayload(t, map[string]any{"text": "hi"}))
	if err != nil {
		t.Fatal(err)
	}
	cmd := DeviceCommand{
		ID:      "cmd-enc-1",
		Kind:    "chat.send",
		Payload: sealed,
	}
	conn.executeAndAck(context.Background(), cmd)

	waitFor(t, func() bool { return mock.ackCount() == 1 }, "ack upload")
	mock.mu.Lock()
	ack := mock.acks["cmd-enc-1"]
	mock.mu.Unlock()
	if ack.Status != "ok" {
		t.Fatalf("ack status = %q, result = %v", ack.Status, ack.Result)
	}
	// The result crossed the wire as an envelope; decrypt it.
	raw, err := json.Marshal(ack.Result)
	if err != nil {
		t.Fatal(err)
	}
	if !IsEnvelope(raw) {
		t.Fatalf("ack result is not an envelope: %s", raw)
	}
	plain, err := conn.cipher.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.Unmarshal(plain, &result); err != nil {
		t.Fatal(err)
	}
	if result["session_id"] != local.createdSessionID {
		t.Fatalf("decrypted ack result = %v, want session_id %q", result, local.createdSessionID)
	}
}

func TestDownlinkEncryptedPayloadDecrypted(t *testing.T) {
	local, localSrv := newFakeLocal(t)
	mock := newMockCloud()
	cloudSrv := httptest.NewServer(mock.handler())
	defer cloudSrv.Close()
	cipher := wiringCipher(t)
	conn := withCipher(newTestConnector(t, cloudSrv.URL, localSrv.URL), cipher)

	// The console seals the command payload with the account CEK.
	sealed, err := cipher.Seal(mustPayload(t, map[string]any{"text": "encrypted hello", "channel": "console"}))
	if err != nil {
		t.Fatal(err)
	}
	cmd := DeviceCommand{ID: "cmd-enc-2", Kind: "chat.send", Payload: sealed}
	conn.executeAndAck(context.Background(), cmd)

	waitFor(t, func() bool { return mock.ackCount() == 1 }, "ack upload")
	local.mu.Lock()
	if len(local.chatBodies) != 1 {
		local.mu.Unlock()
		t.Fatalf("local chat calls = %d, want 1", len(local.chatBodies))
	}
	body := local.chatBodies[0]
	local.mu.Unlock()
	if body["message"] != "encrypted hello" || body["source"] != "console" {
		t.Fatalf("local chat body = %v, want decrypted payload", body)
	}
	mock.mu.Lock()
	status := mock.acks["cmd-enc-2"].Status
	mock.mu.Unlock()
	if status != "ok" {
		t.Fatalf("ack status = %q, want ok", status)
	}
}

func TestDownlinkPlaintextRejectedWithCipherActive(t *testing.T) {
	local, localSrv := newFakeLocal(t)
	mock := newMockCloud()
	cloudSrv := httptest.NewServer(mock.handler())
	defer cloudSrv.Close()
	conn := withCipher(newTestConnector(t, cloudSrv.URL, localSrv.URL), wiringCipher(t))

	cmd := DeviceCommand{ID: "cmd-grey-1", Kind: "chat.send", Payload: mustPayload(t, map[string]any{"text": "must not run"})}
	conn.executeAndAck(context.Background(), cmd)
	waitFor(t, func() bool { return mock.ackCount() == 1 }, "plaintext rejection ack")
	mock.mu.Lock()
	ack := mock.acks[cmd.ID]
	mock.mu.Unlock()
	if ack.Status != "error" {
		t.Fatalf("plaintext command ack = %q, want error", ack.Status)
	}
	local.mu.Lock()
	defer local.mu.Unlock()
	if len(local.chatBodies) != 0 {
		t.Fatalf("plaintext command reached local control plane: %v", local.chatBodies)
	}
}

func TestDownlinkPlaintextAcceptedWhenCipherExplicitlyDisabled(t *testing.T) {
	conn := withCipher(newTestConnector(t, "http://127.0.0.1:1", "http://127.0.0.1:1"), wiringCipher(t))
	conn.cfg.CipherDisabled = true
	payload := mustPayload(t, map[string]any{"text": "explicit grey path"})
	plain, err := conn.openDownlink(payload)
	if err != nil || !bytes.Equal(plain, payload) {
		t.Fatalf("disabled cipher plaintext = %s err=%v", plain, err)
	}
}

func TestDownlinkEnvelopeWithoutCipherRejected(t *testing.T) {
	mock := newMockCloud()
	cloudSrv := httptest.NewServer(mock.handler())
	defer cloudSrv.Close()
	conn := newTestConnector(t, cloudSrv.URL, "http://127.0.0.1:1") // no cipher

	sealed, err := wiringCipher(t).Seal(mustPayload(t, map[string]any{"text": "hi"}))
	if err != nil {
		t.Fatal(err)
	}
	cmd := DeviceCommand{ID: "cmd-enc-3", Kind: "chat.send", Payload: sealed}
	conn.executeAndAck(context.Background(), cmd)

	waitFor(t, func() bool { return mock.ackCount() == 1 }, "error ack")
	mock.mu.Lock()
	ack := mock.acks["cmd-enc-3"]
	mock.mu.Unlock()
	if ack.Status != "error" {
		t.Fatalf("ack status = %q, want error (encrypted command, no CEK)", ack.Status)
	}
}
