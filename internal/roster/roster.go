// Package roster flattens a group's members for display and writes them
// as CSV.
package roster

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

var csvHeader = []string{
	"member_id",
	"first_name",
	"last_name",
	"email",
	"phone",
	"date_of_birth",
	"gender",
	"address",
	"subgroups",
	"roles",
	"guardians",
	"profile_id",
	"has_app_account",
	"created",
}

// Row is a member flattened for display and export, with subgroup,
// role and guardian references resolved to names.
type Row struct {
	Member    spondapi.Membership
	Subgroups string
	Roles     string
	Guardians string
}

func Rows(g spondapi.Group, members []spondapi.Membership) []Row {
	subgroups := make(map[string]string, len(g.SubGroups))
	for _, sg := range g.SubGroups {
		subgroups[sg.ID] = sg.Name
	}
	roles := make(map[string]string, len(g.Roles))
	for _, r := range g.Roles {
		roles[r.ID] = r.Name
	}

	rows := make([]Row, 0, len(members))
	for _, m := range members {
		guardians := make([]string, 0, len(m.Guardians))
		for _, gd := range m.Guardians {
			guardians = append(guardians, describeGuardian(gd))
		}
		rows = append(rows, Row{
			Member:    m,
			Subgroups: strings.Join(Lookup(m.SubGroups, subgroups), "; "),
			Roles:     strings.Join(Lookup(m.Roles, roles), "; "),
			Guardians: strings.Join(guardians, "; "),
		})
	}
	return rows
}

// InSubgroup keeps the members whose subGroups include subgroupID. The members
// endpoint has no subgroup filter; the app filters locally the same way.
func InSubgroup(members []spondapi.Membership, subgroupID string) []spondapi.Membership {
	out := []spondapi.Membership{}
	for _, m := range members {
		if slices.Contains(m.SubGroups, subgroupID) {
			out = append(out, m)
		}
	}
	return out
}

// Lookup maps ids to names, keeping the id when no name is known.
func Lookup(ids []string, names map[string]string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := names[id]; ok {
			out = append(out, name)
			continue
		}
		out = append(out, id)
	}
	return out
}

func describeGuardian(g spondapi.Guardian) string {
	contact := []string{}
	for _, c := range []string{g.Email, g.PhoneNumber} {
		if c != "" {
			contact = append(contact, c)
		}
	}
	if len(contact) == 0 {
		return g.Name()
	}
	return fmt.Sprintf("%s (%s)", g.Name(), strings.Join(contact, ", "))
}

func WriteCSV(w io.Writer, rows []Row) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range rows {
		profileID := ""
		if r.Member.Profile != nil {
			profileID = r.Member.Profile.ID
		}
		record := []string{
			r.Member.ID,
			r.Member.FirstName,
			r.Member.LastName,
			r.Member.Email,
			r.Member.PhoneNumber,
			r.Member.DateOfBirth,
			r.Member.Gender,
			strings.Join(r.Member.Address, ", "),
			r.Subgroups,
			r.Roles,
			r.Guardians,
			profileID,
			fmt.Sprint(r.Member.Profile != nil),
			r.Member.CreatedTime,
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ExportCSV writes rows to path, creating or truncating it.
func ExportCSV(path string, rows []Row) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return WriteCSV(f, rows)
}

var unsafeFileChars = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

// DefaultCSVName derives a file name such as "my-team-members.csv".
func DefaultCSVName(groupName string) string {
	base := strings.Trim(unsafeFileChars.ReplaceAllString(strings.ToLower(groupName), "-"), "-")
	if base == "" {
		base = "group"
	}
	return base + "-members.csv"
}
