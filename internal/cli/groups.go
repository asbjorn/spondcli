package cli

import (
	"fmt"
	"strings"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// findGroup matches by id, then by case-insensitive name.
func findGroup(groups []spondapi.Group, query string) (spondapi.Group, error) {
	if query != "" {
		for _, g := range groups {
			if g.ID == query || strings.EqualFold(g.Name, query) {
				return g, nil
			}
		}
	}

	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, fmt.Sprintf("  %s  (%s)", g.Name, g.ID))
	}
	if query == "" {
		return spondapi.Group{}, fmt.Errorf("-group is required; your groups:\n%s", strings.Join(names, "\n"))
	}
	return spondapi.Group{}, fmt.Errorf("no group %q; your groups:\n%s", query, strings.Join(names, "\n"))
}

// findSubgroup matches a subgroup of g by id, then by case-insensitive name.
func findSubgroup(g spondapi.Group, query string) (*spondapi.Subgroup, error) {
	for i, sg := range g.SubGroups {
		if sg.ID == query || strings.EqualFold(sg.Name, query) {
			return &g.SubGroups[i], nil
		}
	}
	if len(g.SubGroups) == 0 {
		return nil, fmt.Errorf("group %q has no subgroups", g.Name)
	}
	names := make([]string, 0, len(g.SubGroups))
	for _, sg := range g.SubGroups {
		names = append(names, fmt.Sprintf("  %s  (%s)", sg.Name, sg.ID))
	}
	return nil, fmt.Errorf("no subgroup %q in %s; subgroups:\n%s", query, g.Name, strings.Join(names, "\n"))
}
