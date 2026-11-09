package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/roster"
	"github.com/asbjorn/spondcli/internal/spondapi"
	"github.com/asbjorn/spondcli/internal/store"
)

func runSearch(ctx context.Context, args []string) error {
	fs := newFlagSet("search")
	groupArg := fs.String("group", "", "group name or id (case-insensitive name match)")
	subgroupArg := fs.String("subgroup", "", "only posts sent to this subgroup (name or id)")
	refresh := fs.Bool("refresh", false, "sync the group's posts even if the cache is recent")
	if err := parseFlags(fs, args); err != nil {
		return ignoreHelp(err)
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("%w: search: give the words to search for after the flags", ErrUsage)
	}

	st, err := store.OpenDefault()
	if err != nil {
		return err
	}
	defer st.Close()

	c := newSessionClient()
	if err := promptLogin(ctx, c, bufio.NewReader(os.Stdin)); err != nil {
		return err
	}
	groups, err := c.Groups(ctx)
	if err != nil {
		return fmt.Errorf("listing groups: %w", err)
	}
	g, err := findGroup(groups, *groupArg)
	if err != nil {
		return err
	}
	subgroupID := ""
	if *subgroupArg != "" {
		sub, err := findSubgroup(g, *subgroupArg)
		if err != nil {
			return err
		}
		subgroupID = sub.ID
	}

	stats, usable, err := st.Freshen(ctx, c, g, *refresh)
	switch {
	case err != nil && !usable:
		return err
	case err != nil:
		fmt.Fprintln(os.Stderr, "warning: searching cached posts:", err)
	case stats != nil:
		fmt.Fprintf(os.Stderr, "Synced %s (%d new, %d changed, %d removed)\n",
			display.Plural(stats.Posts, "post"), stats.Added, stats.Changed, stats.Removed)
	}

	hits, err := st.Search(ctx, g.ID, subgroupID, query)
	if err != nil {
		return err
	}
	on, off := "", ""
	if term.IsTerminal(int(os.Stdout.Fd())) {
		on, off = "\x1b[1m", "\x1b[22m" // bold
	}
	names := spondapi.MemberNames(g)
	subgroups := make(map[string]string, len(g.SubGroups))
	for _, sg := range g.SubGroups {
		subgroups[sg.ID] = sg.Name
	}
	for _, h := range hits {
		author, ok := names[h.Post.OwnerID]
		if !ok {
			author = h.Post.OwnerID
		}
		line := display.Time(h.Post.Timestamp) + "  " + author
		if len(h.Post.SubGroupIDs) > 0 {
			line += "  [" + strings.Join(roster.Lookup(h.Post.SubGroupIDs, subgroups), ", ") + "]"
		}
		fmt.Println(line)
		if h.Post.Title != "" {
			fmt.Println("  " + store.Unmark(h.Title, on, off))
		}
		fmt.Println("  " + strings.ReplaceAll(store.Unmark(h.Snippet, on, off), "\n", " "))
		fmt.Println()
	}
	fmt.Printf("%s match %q in %s\n", display.Plural(len(hits), "post"), query, g.Name)
	return nil
}
