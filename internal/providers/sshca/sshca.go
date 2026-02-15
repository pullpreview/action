package sshca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Resolution struct {
	Value      string
	EnvKey     string
	UsedLegacy bool
}

type Parsed struct {
	Signer         ssh.Signer
	PublicKey      string
	Source         string
	SourceFromFile bool
}

func ResolveFromEnv(env map[string]string, canonical string, legacy ...string) Resolution {
	canonical = strings.TrimSpace(canonical)
	if canonical != "" {
		if value := strings.TrimSpace(env[canonical]); value != "" {
			return Resolution{Value: value, EnvKey: canonical}
		}
	}
	for _, key := range legacy {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(env[key]); value != "" {
			return Resolution{Value: value, EnvKey: key, UsedLegacy: true}
		}
	}
	return Resolution{EnvKey: canonical}
}

func Parse(raw string, envName string) (Parsed, error) {
	raw = strings.TrimSpace(raw)
	envName = strings.TrimSpace(envName)
	if envName == "" {
		envName = "PULLPREVIEW_CA_KEY"
	}
	if raw == "" {
		return Parsed{}, fmt.Errorf("%s is required", envName)
	}

	source := "inline " + envName
	sourceFromFile := false
	data := []byte(raw)

	if info, err := os.Stat(raw); err == nil {
		if info.IsDir() {
			return Parsed{}, fmt.Errorf("%s %q refers to a directory", envName, raw)
		}
		source = raw
		sourceFromFile = true
		data, err = os.ReadFile(raw)
		if err != nil {
			return Parsed{}, fmt.Errorf("failed to read %s from %q: %w", envName, raw, err)
		}
	}

	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		prefix := "inline " + envName
		if sourceFromFile {
			prefix = fmt.Sprintf("%s file %q", envName, source)
		}
		return Parsed{}, fmt.Errorf("invalid %s: %w", prefix, err)
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if publicKey == "" {
		errPrefix := "inline " + envName
		if sourceFromFile {
			errPrefix = fmt.Sprintf("%s file %q", envName, source)
		}
		return Parsed{}, fmt.Errorf("invalid %s: unable to derive public key", errPrefix)
	}

	return Parsed{
		Signer:         signer,
		PublicKey:      publicKey,
		Source:         source,
		SourceFromFile: sourceFromFile,
	}, nil
}

func GenerateSSHKeyPairWithSigner() (string, string, ssh.Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", nil, err
	}
	private := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if private == nil {
		return "", "", nil, fmt.Errorf("unable to marshal private key")
	}
	public, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return "", "", nil, err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))), strings.TrimSpace(string(private)), signer, nil
}

func GenerateUserCertificate(caSigner ssh.Signer, userSigner ssh.Signer, principal string, ttl time.Duration) (string, error) {
	if caSigner == nil {
		return "", fmt.Errorf("missing CA signer")
	}
	if userSigner == nil {
		return "", fmt.Errorf("missing user signer")
	}
	publicKey := userSigner.PublicKey()
	if publicKey == nil {
		return "", fmt.Errorf("user signer has no public key")
	}
	principal = strings.TrimSpace(principal)
	if principal == "" {
		principal = "user"
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	cert := &ssh.Certificate{
		Key:             publicKey,
		Serial:          uint64(time.Now().UnixNano()),
		CertType:        ssh.UserCert,
		KeyId:           fmt.Sprintf("pullpreview-%s-%d", sanitizePrincipal(principal), time.Now().UnixNano()),
		ValidPrincipals: []string{principal},
		ValidAfter:      uint64(time.Now().Add(-time.Minute).Unix()),
		ValidBefore:     uint64(time.Now().Add(ttl).Unix()),
	}
	if err := cert.SignCert(rand.Reader, caSigner); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(cert))), nil
}

func sanitizePrincipal(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "user"
	}
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-':
			return r
		default:
			return '-'
		}
	}, value)
	value = strings.Trim(value, "-")
	if value == "" {
		value = "user"
	}
	return value
}
