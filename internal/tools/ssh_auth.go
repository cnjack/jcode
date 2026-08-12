package tools

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// BuildSSHAuthMethods builds a list of SSH auth methods by checking the
// SSH agent socket and common private key files in ~/.ssh/.
func BuildSSHAuthMethods() []ssh.AuthMethod {
	var signers []ssh.Signer

	// Try SSH agent (only if it actually holds keys)
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if agentSigners, err := agentSocketSigners(sock); err == nil {
			signers = append(signers, agentSigners...)
		}
	}

	// Try common key files
	keyPaths := []string{
		os.Getenv("HOME") + "/.ssh/id_rsa",
		os.Getenv("HOME") + "/.ssh/id_ed25519",
		os.Getenv("HOME") + "/.ssh/id_ecdsa",
	}
	for _, keyPath := range keyPaths {
		key, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}

	// x/crypto/ssh considers authentication methods by protocol name. Multiple
	// ssh.PublicKeys AuthMethods are all named "publickey", so after trying the
	// first one it skips the rest. Put every agent/default-key signer into one
	// AuthMethod so the SSH package can try each identity in order.
	if len(signers) == 0 {
		return nil
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}
}

// agentSocketSigners snapshots the public keys currently offered by an SSH
// agent without tying the returned signers to the short-lived List connection.
// The signers reconnect for each signature request. This matters because the
// signers returned by agent.Client.Signers retain the client internally; closing
// that client's socket before ssh.NewClientConn finishes authentication makes
// every agent-backed login fail with a closed-pipe error.
func agentSocketSigners(socketPath string) ([]ssh.Signer, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, err
	}

	keys, err := sshagent.NewClient(conn).List()
	if err != nil {
		return nil, err
	}
	signers := make([]ssh.Signer, 0, len(keys))
	for _, key := range keys {
		signers = append(signers, &agentSocketSigner{socketPath: socketPath, publicKey: key})
	}
	return signers, nil
}

// agentSocketSigner is deliberately connectionless between calls. It avoids a
// process-lifetime fd leak while keeping agent signatures usable after auth
// method discovery has returned.
type agentSocketSigner struct {
	socketPath string
	publicKey  ssh.PublicKey
}

func (s *agentSocketSigner) PublicKey() ssh.PublicKey { return s.publicKey }

func (s *agentSocketSigner) Sign(_ io.Reader, data []byte) (*ssh.Signature, error) {
	return s.sign(data, "")
}

func (s *agentSocketSigner) SignWithAlgorithm(_ io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	return s.sign(data, algorithm)
}

func (s *agentSocketSigner) sign(data []byte, algorithm string) (*ssh.Signature, error) {
	conn, err := net.DialTimeout("unix", s.socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to SSH agent: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("set SSH agent deadline: %w", err)
	}

	agent := sshagent.NewClient(conn)
	switch algorithm {
	case "":
		return agent.Sign(s.publicKey, data)
	case ssh.KeyAlgoRSASHA256:
		return agent.SignWithFlags(s.publicKey, data, sshagent.SignatureFlagRsaSha256)
	case ssh.KeyAlgoRSASHA512:
		return agent.SignWithFlags(s.publicKey, data, sshagent.SignatureFlagRsaSha512)
	default:
		if algorithm == agentUnderlyingAlgorithm(s.publicKey.Type()) {
			return agent.Sign(s.publicKey, data)
		}
		return nil, fmt.Errorf("SSH agent does not support signature algorithm %q", algorithm)
	}
}

func agentUnderlyingAlgorithm(keyType string) string {
	switch keyType {
	case ssh.CertAlgoRSAv01:
		return ssh.KeyAlgoRSA
	case ssh.CertAlgoRSASHA256v01:
		return ssh.KeyAlgoRSASHA256
	case ssh.CertAlgoRSASHA512v01:
		return ssh.KeyAlgoRSASHA512
	case ssh.CertAlgoECDSA256v01:
		return ssh.KeyAlgoECDSA256
	case ssh.CertAlgoECDSA384v01:
		return ssh.KeyAlgoECDSA384
	case ssh.CertAlgoECDSA521v01:
		return ssh.KeyAlgoECDSA521
	case ssh.CertAlgoSKECDSA256v01:
		return ssh.KeyAlgoSKECDSA256
	case ssh.CertAlgoED25519v01:
		return ssh.KeyAlgoED25519
	case ssh.CertAlgoSKED25519v01:
		return ssh.KeyAlgoSKED25519
	default:
		return keyType
	}
}

var _ ssh.AlgorithmSigner = (*agentSocketSigner)(nil)
