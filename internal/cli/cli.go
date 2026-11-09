// Package cli implements the spondcli command line: argument parsing,
// subcommands and the hand-off to the interactive UI.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/asbjorn/spondcli/internal/store"
	"github.com/asbjorn/spondcli/internal/tui"
)

// ErrUsage marks errors caused by a malformed command line. The caller
// should exit with status 2 for them, as the flag package does.
var ErrUsage = errors.New("usage error")

// version is set at build time:
//
//	go build -ldflags "-X github.com/asbjorn/spondcli/internal/cli.version=v1.2.3" ./cmd/spondcli
var version = ""

// command is one subcommand.
type command struct {
	name    string
	args    string // synopsis of the arguments, for the usage text
	summary string
	run     func(ctx context.Context, args []string) error
}

func commands() []command {
	return []command{
		{"members", "-group NAME|ID [-subgroup NAME|ID] [-o FILE]",
			"list a group's (or subgroup's) members, optionally export CSV", runMembers},
		{"search", "-group NAME|ID [-subgroup NAME|ID] [-refresh] WORDS...",
			"search posts and comments in the local cache, synced when older than 15 minutes", runSearch},
		{"logout", "", "forget the cached login", runLogout},
		{"version", "", "print the version", runVersion},
		{"help", "[COMMAND]", "show help for spondcli or a command", runHelp},
	}
}

const footer = `Without a command, spondcli starts the interactive UI with the Home,
Alerts, Groups and Messages tabs (keys 1-4).

Credentials are read from SPOND_EMAIL and SPOND_PASSWORD, or prompted for.
After logging in, the access token is cached in the user config directory
(%AppData%, ~/Library/Application Support or ~/.config) under spondcli/,
and reused until it expires. Posts are cached in the user cache directory
(%LocalAppData%, ~/Library/Caches or ~/.cache) under spondcli/posts.db.
`

// Run runs spondcli with args, the command line without the program name.
func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return runTUI(ctx)
	}
	switch args[0] {
	case "-h", "-help", "--help":
		printUsage(os.Stdout)
		return nil
	case "-version", "--version":
		return runVersion(ctx, nil)
	}
	cmd, ok := lookup(args[0])
	if !ok {
		printUsage(os.Stderr)
		return fmt.Errorf("%w: unknown command %q", ErrUsage, args[0])
	}
	return cmd.run(ctx, args[1:])
}

func lookup(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "spondcli is a terminal client for Spond.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  spondcli                 start the interactive UI")
	fmt.Fprintln(w, "  spondcli COMMAND [ARGS]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprint(w, footer)
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Run "spondcli help COMMAND" for a command's flags.`)
}

// newFlagSet returns a flag set for cmd whose -h output shows its synopsis.
func newFlagSet(name string) *flag.FlagSet {
	cmd, _ := lookup(name)
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		w := fs.Output()
		fmt.Fprintf(w, "Usage: spondcli %s %s\n\n%s.\n", cmd.name, cmd.args, capitalize(cmd.summary))
		if hasFlags(fs) {
			fmt.Fprintln(w, "\nFlags:")
			fs.PrintDefaults()
		}
	}
	return fs
}

// parseFlags parses args into fs. -h prints the command's help and returns
// flag.ErrHelp; other errors are usage errors.
func parseFlags(fs *flag.FlagSet, args []string) error {
	err := fs.Parse(args)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return err
	default:
		return fmt.Errorf("%w: %s: %v", ErrUsage, fs.Name(), err)
	}
}

func hasFlags(fs *flag.FlagSet) bool {
	n := 0
	fs.VisitAll(func(*flag.Flag) { n++ })
	return n > 0
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func runTUI(ctx context.Context) error {
	st, storeErr := store.OpenDefault()
	if st != nil {
		defer st.Close()
	}
	return tui.Run(ctx, newSessionClient(), st, storeErr)
}

func runLogout(_ context.Context, args []string) error {
	fs := newFlagSet("logout")
	if err := parseFlags(fs, args); err != nil {
		return ignoreHelp(err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: logout takes no arguments", ErrUsage)
	}
	return newSessionClient().Logout()
}

func runVersion(_ context.Context, _ []string) error {
	fmt.Println("spondcli", buildVersion())
	return nil
}

// buildVersion is the -ldflags version, else what the Go toolchain recorded:
// the module version for "go install …@vX", or the VCS revision.
func buildVersion() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "devel"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return "devel " + rev
}

func runHelp(_ context.Context, args []string) error {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}
	cmd, ok := lookup(args[0])
	if !ok {
		return fmt.Errorf("%w: unknown command %q", ErrUsage, args[0])
	}
	if cmd.name == "help" || cmd.name == "version" {
		fmt.Printf("Usage: spondcli %s %s\n\n%s.\n", cmd.name, cmd.args, capitalize(cmd.summary))
		return nil
	}
	return ignoreHelp(cmd.run(context.Background(), []string{"-h"}))
}

// ignoreHelp turns flag.ErrHelp (the user asked for -h) into success.
func ignoreHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}
