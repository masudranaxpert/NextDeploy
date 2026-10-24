package handlers

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var (
	localSSHMu   sync.Mutex
	localSSHKey  ssh.Signer
	localSSHPKey string
)

// EnsureLocalHostSSH prepares an ed25519 key pair in dataDir and syncs the public key
// to the host's /root/.ssh/authorized_keys via a rapid nsenter call if not already authorized.
func EnsureLocalHostSSH(dataDir string) (ssh.Signer, error) {
	localSSHMu.Lock()
	defer localSSHMu.Unlock()

	if localSSHKey != nil {
		return localSSHKey, nil
	}

	keyPath := filepath.Join(dataDir, "id_ed25519_local")
	pubPath := keyPath + ".pub"

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		// Generate fresh ed25519 key
		pub, priv, gerr := ed25519.GenerateKey(rand.Reader)
		if gerr != nil {
			return nil, fmt.Errorf("generate key: %w", gerr)
		}

		privBytes, merr := ssh.MarshalPrivateKey(priv, "nextdeploy-local-terminal")
		if merr != nil {
			return nil, fmt.Errorf("marshal priv key: %w", merr)
		}
		privPEM := pem.EncodeToMemory(privBytes)
		if err := os.WriteFile(keyPath, privPEM, 0600); err != nil {
			return nil, fmt.Errorf("write priv key: %w", err)
		}

		sshPub, perr := ssh.NewPublicKey(pub)
		if perr != nil {
			return nil, fmt.Errorf("parse pub key: %w", perr)
		}
		pubAuthorized := string(ssh.MarshalAuthorizedKey(sshPub))
		_ = os.WriteFile(pubPath, []byte(pubAuthorized), 0644)
		keyBytes = privPEM
		localSSHPKey = strings.TrimSpace(pubAuthorized)
	} else {
		if pubBytes, rerr := os.ReadFile(pubPath); rerr == nil {
			localSSHPKey = strings.TrimSpace(string(pubBytes))
		}
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	localSSHKey = signer

	// Authorize key on host via background nsenter if needed
	go syncKeyToHost(localSSHPKey)

	return signer, nil
}

// syncKeyToHost appends NextDeploy's public key to the host's /root/.ssh/authorized_keys
func syncKeyToHost(pubKey string) {
	if pubKey == "" {
		return
	}
	// Try up to 3 times to install the public key to host
	for i := 0; i < 3; i++ {
		cmdStr := fmt.Sprintf(`mkdir -p /root/.ssh && chmod 700 /root/.ssh && touch /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys && grep -qF "%s" /root/.ssh/authorized_keys || echo "%s" >> /root/.ssh/authorized_keys`, pubKey, pubKey)
		cmd := exec.Command("docker", "run", "--rm", "--entrypoint", "nsenter", "--privileged", "--pid=host", "mrformula/nextdeploy:latest", "-t", "1", "-m", "-u", "-n", "-i", "sh", "-c", cmdStr)
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
}

// DialHostSSH attempts an SSH connection to the host on 127.0.0.1:22 or host gateway
func DialHostSSH(signer ssh.Signer) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         3 * time.Second,
	}

	// Try localhost, host.docker.internal, and standard docker gateway
	targets := []string{"127.0.0.1:22", "host.docker.internal:22", "172.17.0.1:22"}
	for _, target := range targets {
		conn, err := net.DialTimeout("tcp", target, 1500*time.Millisecond)
		if err == nil {
			c, chans, reqs, serr := ssh.NewClientConn(conn, target, config)
			if serr == nil {
				return ssh.NewClient(c, chans, reqs), nil
			}
			_ = conn.Close()
		}
	}

	return nil, fmt.Errorf("host ssh unreachable")
}
