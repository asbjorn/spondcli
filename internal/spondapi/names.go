package spondapi

// MemberNames maps both membership ids and profile ids to display names,
// since it is not verified which one a post's ownerId refers to.
func MemberNames(g Group) map[string]string {
	names := make(map[string]string, 2*len(g.Members))
	for _, m := range g.Members {
		names[m.ID] = m.Name()
		if m.Profile != nil {
			names[m.Profile.ID] = m.Name()
		}
	}
	return names
}
