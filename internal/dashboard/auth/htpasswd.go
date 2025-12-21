package auth

import (
	"bufio"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// HtpasswdAuth handles htpasswd-style authentication
type HtpasswdAuth struct {
	users map[string]string // username -> password hash
}

func NewHtpasswdAuth(input string) (*HtpasswdAuth, error) {
	auth := &HtpasswdAuth{
		users: make(map[string]string),
	}

	if _, err := os.Stat(input); err == nil {
		if err := auth.loadFromFile(input); err != nil {
			return nil, err
		}
		return auth, nil
	}

	if err := auth.parseCredentials(input); err != nil {
		return nil, err
	}

	if len(auth.users) == 0 {
		return nil, fmt.Errorf("no valid credentials found")
	}

	return auth, nil
}

// loadFromFile reads htpasswd credentials from a file
func (a *HtpasswdAuth) loadFromFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open htpasswd file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if err := a.parseLine(line); err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read htpasswd file: %w", err)
	}

	return nil
}

// parseCredentials parses inline credentials (can be multiple lines)
func (a *HtpasswdAuth) parseCredentials(input string) error {
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := a.parseLine(line); err != nil {
			return err
		}
	}
	return nil
}

// parseLine parses a single htpasswd line (user:hash)
func (a *HtpasswdAuth) parseLine(line string) error {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid htpasswd format: expected user:hash")
	}

	username := strings.TrimSpace(parts[0])
	hash := strings.TrimSpace(parts[1])

	if username == "" || hash == "" {
		return fmt.Errorf("invalid htpasswd format: empty username or hash")
	}

	a.users[username] = hash
	return nil
}

// Authenticate checks if the provided username and password are valid
func (a *HtpasswdAuth) Authenticate(username, password string) bool {
	hash, exists := a.users[username]
	if !exists {
		return false
	}

	return checkPassword(password, hash)
}

// checkPassword verifies a password against various htpasswd hash formats
func checkPassword(password, hash string) bool {
	switch {
	case strings.HasPrefix(hash, "$2y$") || strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$"):
		// bcrypt
		err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		return err == nil

	case strings.HasPrefix(hash, "{SHA}"):
		// SHA1 (base64 encoded)
		return checkSHA1(password, hash[5:])

	case strings.HasPrefix(hash, "$apr1$"):
		// Apache MD5 (apr1) - not implemented, use bcrypt instead
		return false

	default:
		// Plain text comparison (not recommended but supported)
		return subtle.ConstantTimeCompare([]byte(password), []byte(hash)) == 1
	}
}

// checkSHA1 verifies a password against a SHA1 hash (base64 encoded)
func checkSHA1(password, encodedHash string) bool {
	decoded, err := base64.StdEncoding.DecodeString(encodedHash)
	if err != nil {
		return false
	}

	h := sha1.New()
	h.Write([]byte(password))
	computed := h.Sum(nil)

	return subtle.ConstantTimeCompare(decoded, computed) == 1
}

// UserCount returns the number of configured users
func (a *HtpasswdAuth) UserCount() int {
	return len(a.users)
}
