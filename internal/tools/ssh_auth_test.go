package tools

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/testdata"
)

func TestAgentSocketSignerReconnectsAfterDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-domain ssh-agent socket test")
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: privateKey}); err != nil {
		t.Fatal(err)
	}

	socketPath := shortUnixSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	var servers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			servers.Add(1)
			go func() {
				defer servers.Done()
				_ = sshagent.ServeAgent(keyring, conn)
				_ = conn.Close()
			}()
		}
	}()

	signers, err := agentSocketSigners(socketPath)
	if err != nil {
		t.Fatalf("agentSocketSigners: %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("got %d signers, want 1", len(signers))
	}
	data := []byte("sign after the discovery socket has closed")
	signature, err := signers[0].Sign(rand.Reader, data)
	if err != nil {
		t.Fatalf("sign via a fresh agent connection: %v", err)
	}
	if err := signers[0].PublicKey().Verify(data, signature); err != nil {
		t.Fatalf("verify agent signature: %v", err)
	}
	if _, ok := signers[0].(ssh.AlgorithmSigner); !ok {
		t.Fatal("agent-backed signer must support negotiated RSA algorithms")
	}

	_ = listener.Close()
	<-done
	servers.Wait()
}

func TestBuildSSHAuthMethodsCombinesAgentAndDefaultKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-domain ssh-agent socket test")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// The agent key is intentionally rejected; the default on-disk key must
	// still be attempted by the same publickey AuthMethod and succeed.
	_, rejectedPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, acceptedPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	acceptedSigner, err := ssh.NewSignerFromKey(acceptedPrivate)
	if err != nil {
		t.Fatal(err)
	}
	pemBlock, err := ssh.MarshalPrivateKey(acceptedPrivate, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		t.Fatal(err)
	}

	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: rejectedPrivate}); err != nil {
		t.Fatal(err)
	}
	socketPath := shortUnixSocketPath(t)
	_, stopAgent := serveTestAgent(t, socketPath, keyring)
	defer stopAgent()
	t.Setenv("SSH_AUTH_SOCK", socketPath)

	authMethods := BuildSSHAuthMethods()
	if len(authMethods) != 1 {
		t.Fatalf("got %d auth methods, want one combined publickey method", len(authMethods))
	}

	sshListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sshListener.Close() }()
	hostSigner, err := ssh.ParsePrivateKey(testdata.PEMBytes["rsa"])
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) != string(acceptedSigner.PublicKey().Marshal()) {
				return nil, fmt.Errorf("rejected test identity")
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(hostSigner)
	serverDone := make(chan error, 1)
	go func() {
		serverSide, acceptErr := sshListener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		conn, _, _, serverErr := ssh.NewServerConn(serverSide, serverConfig)
		if conn != nil {
			_ = conn.Close()
		}
		serverDone <- serverErr
	}()

	clientConfig := &ssh.ClientConfig{
		User:            "test",
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // in-memory auth-only test
		Timeout:         time.Second,
	}
	clientSide, err := net.DialTimeout("tcp", sshListener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	clientConn, _, _, err := ssh.NewClientConn(clientSide, sshListener.Addr().String(), clientConfig)
	if err != nil {
		t.Fatalf("second identity in combined AuthMethod was not attempted: %v", err)
	}
	_ = clientConn.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("SSH test server did not exit")
	}
}

func serveTestAgent(t *testing.T, socketPath string, keyring sshagent.Agent) (net.Listener, func()) {
	t.Helper()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	var servers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			servers.Add(1)
			go func() {
				defer servers.Done()
				_ = sshagent.ServeAgent(keyring, conn)
				_ = conn.Close()
			}()
		}
	}()
	return listener, func() {
		_ = listener.Close()
		<-done
		servers.Wait()
	}
}

func shortUnixSocketPath(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp("/tmp", "jcode-agent-*.sock")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}
