package auth

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Authenticator interface {
	Validate(token string) (clientID string, ok bool)
}

// TokenFileAuth reads tokens from a flat file. Format: "token:label" per line.
// Empty lines and lines starting with # are ignored. Read once at startup.
type TokenFileAuth struct {
	tokens map[string]string
}

func NewTokenFileAuth(path string) (*TokenFileAuth, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open token file: %w", err)
	}
	defer f.Close()

	tokens := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		token, label, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("line %d: expected 'token:label' format", lineNum)
		}

		token = strings.TrimSpace(token)
		label = strings.TrimSpace(label)

		if token == "" {
			return nil, fmt.Errorf("line %d: empty token", lineNum)
		}

		tokens[token] = label
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}

	return &TokenFileAuth{tokens: tokens}, nil
}

func (a *TokenFileAuth) Validate(token string) (string, bool) {
	label, ok := a.tokens[token]
	if !ok {
		return "", false
	}
	return label, true
}

// AllowAll is a no-auth authenticator for testing and development.
type AllowAll struct{}

func (AllowAll) Validate(token string) (string, bool) {
	if token == "" {
		return "anonymous", true
	}
	return token, true
}
