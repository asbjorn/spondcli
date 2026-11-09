package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/roster"
	"github.com/asbjorn/spondcli/internal/spondapi"
)

func runMembers(ctx context.Context, args []string) error {
	fs := newFlagSet("members")
	groupArg := fs.String("group", "", "group name or id (case-insensitive name match)")
	subgroupArg := fs.String("subgroup", "", "only members of this subgroup (name or id)")
	out := fs.String("o", "", "write members to this CSV file")
	if err := parseFlags(fs, args); err != nil {
		return ignoreHelp(err)
	}

	if fs.NArg() > 0 {
		return fmt.Errorf("%w: members: unexpected argument %q", ErrUsage, fs.Arg(0))
	}

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
	title := g.Name
	var sub *spondapi.Subgroup
	if *subgroupArg != "" {
		if sub, err = findSubgroup(g, *subgroupArg); err != nil {
			return err
		}
		title += " › " + sub.Name
	}

	members, err := c.Members(ctx, g.ID)
	if err != nil {
		return fmt.Errorf("listing members: %w", err)
	}
	if sub != nil {
		members = roster.InSubgroup(members, sub.ID)
	}
	rows := roster.Rows(g, members)

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tEMAIL\tPHONE\tSUBGROUPS\tROLES\tGUARDIANS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Member.Name(), r.Member.Email, r.Member.PhoneNumber, r.Subgroups, r.Roles, r.Guardians)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Printf("\n%s in %s\n", display.Plural(len(rows), "member"), title)

	if *out == "" {
		return nil
	}
	if err := roster.ExportCSV(*out, rows); err != nil {
		return fmt.Errorf("writing CSV: %w", err)
	}
	fmt.Printf("Exported to %s\n", *out)
	return nil
}
