package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenFileAuth(t *testing.T) {
	content := `# This is a comment
sk-abc123:jay
sk-deploy:ci-runner

sk-dev:local-dev
`
	path := filepath.Join(t.TempDir(), "tokens.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	auth, err := NewTokenFileAuth(path)
	if err != nil {
		t.Fatalf("NewTokenFileAuth: %v", err)
	}

	tests := []struct {
		token  string
		wantID string
		wantOK bool
	}{
		{"sk-abc123", "jay", true},
		{"sk-deploy", "ci-runner", true},
		{"sk-dev", "local-dev", true},
		{"sk-wrong", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		id, ok := auth.Validate(tt.token)
		if ok != tt.wantOK || id != tt.wantID {
			t.Errorf("Validate(%q) = (%q, %v), want (%q, %v)",
				tt.token, id, ok, tt.wantID, tt.wantOK)
		}
	}
}

func TestTokenFileAuth_BadFormat(t *testing.T) {
	content := "this-has-no-colon\n"
	path := filepath.Join(t.TempDir(), "tokens.txt")
	os.WriteFile(path, []byte(content), 0644)

	_, err := NewTokenFileAuth(path)
	if err == nil {
		t.Fatal("expected error for bad format")
	}
}

func TestTokenFileAuth_MissingFile(t *testing.T) {
	_, err := NewTokenFileAuth("/nonexistent/tokens.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestAllowAll(t *testing.T) {
	a := AllowAll{}

	id, ok := a.Validate("anything")
	if !ok || id != "anything" {
		t.Errorf("Validate('anything') = (%q, %v), want ('anything', true)", id, ok)
	}

	id, ok = a.Validate("")
	if !ok || id != "anonymous" {
		t.Errorf("Validate('') = (%q, %v), want ('anonymous', true)", id, ok)
	}
}
