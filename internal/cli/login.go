package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// newSessionClient returns a client that caches its login and starts with
// the cached token, if any.
func newSessionClient() *spondapi.Client {
	c := spondapi.NewClient()
	path, err := defaultSessionFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: login will not be cached:", err)
		return c
	}
	c.SessionFile = path
	c.Token = spondapi.LoadSession(path)
	return c
}

// promptLogin logs in on a plain terminal, for non-interactive commands. A
// cached token is used when the server still accepts it.
func promptLogin(ctx context.Context, c *spondapi.Client, in *bufio.Reader) error {
	if c.Token != "" {
		_, err := c.Me(ctx)
		if err == nil {
			return nil
		}
		if !spondapi.IsUnauthorized(err) {
			return fmt.Errorf("checking cached login: %w", err)
		}
		if err := c.Logout(); err != nil {
			return fmt.Errorf("clearing expired login: %w", err)
		}
	}

	email := os.Getenv("SPOND_EMAIL")
	if email == "" {
		var err error
		if email, err = prompt(in, "Email: "); err != nil {
			return err
		}
	}

	password := os.Getenv("SPOND_PASSWORD")
	if password == "" {
		fmt.Fprint(os.Stderr, "Password: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("reading password: %w", err)
		}
		password = string(raw)
	}

	resp, err := c.Login(ctx, email, password)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if resp.AccessToken != nil {
		warnIfErr(c.SetToken(resp.AccessToken))
		return nil
	}
	if resp.Token == "" {
		return errors.New("login: response has neither tokens nor a 2FA challenge")
	}

	code, err := prompt(in, fmt.Sprintf("2FA code sent to %s: ", resp.PhoneNumber))
	if err != nil {
		return err
	}
	token, err := c.Verify2FA(ctx, resp.Token, code)
	if err != nil {
		return fmt.Errorf("2FA: %w", err)
	}
	warnIfErr(c.SetToken(token))
	return nil
}

func warnIfErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
}

// prompt writes to stderr so stdout stays clean for redirected output.
func prompt(in *bufio.Reader, label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	line, err := in.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// defaultSessionFile is session.json in the per-user config directory:
// %AppData%\spondcli on Windows, ~/Library/Application Support/spondcli on
// macOS and $XDG_CONFIG_HOME/spondcli (~/.config/spondcli) elsewhere.
func defaultSessionFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "spondcli", "session.json"), nil
}
