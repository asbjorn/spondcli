package spondapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// session is the cached login, so the next run can skip the password and 2FA.
type session struct {
	Token      string `json:"token"`
	Expiration string `json:"expiration"` // RFC 3339, as the login response gives it
}

// expiryMargin drops a cached token this long before it expires, so a
// session does not lapse halfway through a run.
const expiryMargin = 5 * time.Minute

// LoadSession returns the cached access token, or "" when there is none or
// it has expired. A token without a parseable expiration is kept; the server
// rejects it with 401 if it is no longer valid.
func LoadSession(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var s session
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	if exp, err := time.Parse(time.RFC3339Nano, s.Expiration); err == nil && time.Until(exp) < expiryMargin {
		return ""
	}
	return s.Token
}

// saveSession writes the token readable by the current user only. It writes
// a temporary file and renames it, so a crash never leaves a torn file.
func saveSession(path string, t *ExpiableToken) error {
	data, err := json.Marshal(session{Token: t.Token, Expiration: t.Expiration})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SetToken starts using an access token and caches it when the client has a
// session file. Failing to cache is not fatal: the token still works for
// this run.
func (c *Client) SetToken(t *ExpiableToken) error {
	c.Token = t.Token
	if c.SessionFile == "" {
		return nil
	}
	if err := saveSession(c.SessionFile, t); err != nil {
		return fmt.Errorf("caching login: %w", err)
	}
	return nil
}

// Logout forgets the token and deletes the cached session.
func (c *Client) Logout() error {
	c.Token = ""
	if c.SessionFile == "" {
		return nil
	}
	if err := os.Remove(c.SessionFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// IsUnauthorized reports whether err is the API rejecting the access token.
func IsUnauthorized(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}
