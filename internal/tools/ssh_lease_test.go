package tools

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHCloneLeaseKeepsTransportAlive(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("loopback listener is unavailable")
	}
	addr, hostKey, clientSigner, serverDone := startLeaseTestSSHServer(t)
	exec, err := NewSSHExecutorContext(
		context.Background(),
		addr,
		"test",
		[]ssh.AuthMethod{ssh.PublicKeys(clientSigner)},
		ssh.FixedHostKey(hostKey),
	)
	if err != nil {
		t.Fatalf("connect test SSH server: %v", err)
	}

	lease, err := exec.CloneLease()
	if err != nil {
		t.Fatalf("clone SSH lease: %v", err)
	}
	if err := exec.Close(); err != nil {
		t.Fatalf("close first lease: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := lease.Probe(ctx); err != nil {
		t.Fatalf("shared transport died with first lease: %v", err)
	}
	stdout, stderr, err := lease.Exec(ctx, "printf clone-ok", "", time.Second)
	if err != nil {
		t.Fatalf("execute through cloned lease: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, "clone-ok") {
		t.Fatalf("cloned lease output = %q, want clone-ok", stdout)
	}
	if err := lease.Close(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("close final lease: %v", err)
	}
	select {
	case err := <-serverDone:
		if err != nil && !strings.Contains(err.Error(), "EOF") {
			t.Fatalf("test SSH server: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("final lease did not close shared SSH transport")
	}
}

func startLeaseTestSSHServer(t *testing.T) (string, ssh.PublicKey, ssh.Signer, <-chan error) {
	t.Helper()
	_, hostPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	_, clientPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) != string(clientSigner.PublicKey().Marshal()) {
				return nil, fmt.Errorf("unexpected client key")
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(hostSigner)
	done := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		serverConn, channels, requests, handshakeErr := ssh.NewServerConn(conn, serverConfig)
		if handshakeErr != nil {
			done <- handshakeErr
			return
		}
		go func() {
			for req := range requests {
				_ = req.Reply(false, nil)
			}
		}()
		go serveLeaseTestChannels(channels)
		done <- serverConn.Wait()
	}()
	return listener.Addr().String(), hostSigner.PublicKey(), clientSigner, done
}

func serveLeaseTestChannels(channels <-chan ssh.NewChannel) {
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "session channels only")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer func() { _ = channel.Close() }()
			for req := range requests {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var payload struct{ Command string }
				if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
					_ = req.Reply(false, nil)
					return
				}
				_ = req.Reply(true, nil)
				switch {
				case strings.Contains(payload.Command, "uname -sm"):
					_, _ = io.WriteString(channel, "Linux x86_64\n")
				case strings.Contains(payload.Command, "clone-ok"):
					_, _ = io.WriteString(channel, "clone-ok")
				}
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			}
		}()
	}
}
