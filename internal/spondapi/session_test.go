package spondapi

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSessionRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spondcli", "session.json")
	c := NewClient()
	c.SessionFile = path

	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if err := c.SetToken(&ExpiableToken{Token: "abc", Expiration: exp}); err != nil {
		t.Fatal(err)
	}
	if got := LoadSession(path); got != "abc" {
		t.Errorf("loadSession = %q, want abc", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("session file mode = %o, want 600", perm)
		}
	}

	if err := c.Logout(); err != nil {
		t.Fatal(err)
	}
	if c.Token != "" || LoadSession(path) != "" {
		t.Errorf("logout kept the session")
	}
	if err := c.Logout(); err != nil {
		t.Errorf("second logout: %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	soon := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	if err := saveSession(path, &ExpiableToken{Token: "abc", Expiration: soon}); err != nil {
		t.Fatal(err)
	}
	if got := LoadSession(path); got != "" {
		t.Errorf("token expiring within the margin was loaded: %q", got)
	}

	if err := saveSession(path, &ExpiableToken{Token: "abc"}); err != nil {
		t.Fatal(err)
	}
	if got := LoadSession(path); got != "abc" {
		t.Errorf("token without expiration = %q, want abc (server decides)", got)
	}
}
