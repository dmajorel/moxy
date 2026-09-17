package maintenance

import (
	"context"
	"encoding/pem"
	"errors"
	"strings"
)

// SSHKeySource is the "ssh-key" mode: one ed25519 private key, read once from
// the file the configuration names, handed out unchanged for every execution.
//
// It answers the same credential every time, and that is exactly right. The key
// does not rotate, the file does not change while the process runs, and there
// is nothing to mint: re-reading it per execution would only add a way for a
// daemon that works to stop working when somebody moves a file. The
// KeyProvider interface is called per execution for the other implementation's
// sake -- OpenBao mints a short-lived certificate -- and this one satisfies it
// by being trivial rather than by being special-cased anywhere downstream.
//
// What it deliberately does NOT do: touch the filesystem. The file is read,
// its permissions and its ownership are checked, and it is wrapped at load
// time by internal/config, because that is the one moment when the problem is
// cheap to see. This type takes the bytes.
type SSHKeySource struct {
	// cred is never handed out by pointer: Credential returns a fresh struct
	// so that no caller can blank the field of a provider shared by every
	// execution of the process.
	cred Credential
}

// Errors of the key source. They stay this coarse on purpose: they are read by
// an operator at start-up, not matched on by code.
var (
	errEmptyKey     = errors.New("private key is empty")
	errNotPEM       = errors.New("private key is not PEM")
	errEncryptedKey = errors.New("private key is encrypted: maintenance needs a passphrase-less key")
)

// NewSSHKeySource wraps an already-read PEM private key.
//
// The two checks it makes are structural, not a second validation pass: a
// provider that hands out bytes no session can ever use would turn a
// misconfiguration into a failure at the first click, weeks later, on the
// night somebody needs to drain a node.
func NewSSHKeySource(privateKeyPEM []byte) (*SSHKeySource, error) {
	if len(privateKeyPEM) == 0 {
		return nil, errEmptyKey
	}
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, errNotPEM
	}
	if encryptedPEM(block) {
		return nil, errEncryptedKey
	}
	return &SSHKeySource{cred: Credential{PrivateKeyPEM: privateKeyPEM}}, nil
}

// Credential implements KeyProvider. It never fails and never blocks, so it
// ignores the context: there is nothing to cancel.
func (s *SSHKeySource) Credential(_ context.Context) (*Credential, error) {
	cred := s.cred
	return &cred, nil
}

// encryptedPEM reports whether the block is a passphrase-protected key.
//
// Two shapes exist and both have to be caught: the classic PEM encryption
// headers that OpenSSL-style keys carry, and the OpenSSH format, which puts no
// header at all and records its cipher inside the base64 body -- so an
// encrypted OpenSSH key looks exactly like a plain one from the outside.
// Matching the cipher name in the decoded bytes is crude, and it is what tells
// the two apart without an SSH library.
func encryptedPEM(block *pem.Block) bool {
	if _, ok := block.Headers["DEK-Info"]; ok {
		return true
	}
	if proc, ok := block.Headers["Proc-Type"]; ok && strings.Contains(proc, "ENCRYPTED") {
		return true
	}
	if strings.Contains(block.Type, "ENCRYPTED") {
		return true
	}
	if !strings.Contains(block.Type, "OPENSSH") {
		return false
	}
	// An OpenSSH private key announces its cipher right after the magic
	// "openssh-key-v1\x00"; "none" is the passphrase-less one.
	const magic = "openssh-key-v1\x00"
	body := string(block.Bytes)
	if !strings.HasPrefix(body, magic) {
		return false
	}
	return !strings.HasPrefix(body[len(magic):], "\x00\x00\x00\x04none")
}
