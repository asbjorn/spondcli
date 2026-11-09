package tui

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/roster"
	"github.com/asbjorn/spondcli/internal/spondapi"
)

// Wire names of ApiGroupRole.Permission.
const (
	permMembers = "members"
	permAdmins  = "admins"
)

// permissions is what the logged-in user may do in a group.
//
// It mirrors the app's DbGroupPermissions view: take the memberships whose
// profile is the user's own (isSelf) and whose admin role is not pending,
// and combine the permissions of their roles. Only admin roles (ApiGroup.roles;
// roles[0] is the super-admin role) grant management permissions; the
// member and guardian roles never do (GroupMapperKt.createDbGroupRole).
type permissions struct {
	manageMembers bool
	manageAdmins  bool
}

func myPermissions(g spondapi.Group, me *spondapi.MyProfile) permissions {
	if me == nil || me.ID == "" {
		return permissions{}
	}
	roles := make(map[string]spondapi.Role, len(g.Roles))
	for _, r := range g.Roles {
		roles[r.ID] = r
	}

	var p permissions
	seen := map[string]bool{}
	candidates := append([]spondapi.Membership{}, g.Members...)
	candidates = append(candidates, g.BehalfOfMemberships...)
	if g.MyMembership != nil {
		candidates = append(candidates, *g.MyMembership)
	}
	for _, m := range candidates {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		if m.Profile == nil || m.Profile.ID != me.ID || m.PendingAdmin {
			continue
		}
		for _, id := range m.Roles {
			r := roles[id]
			p.manageMembers = p.manageMembers || slices.Contains(r.Permissions, permMembers)
			p.manageAdmins = p.manageAdmins || slices.Contains(r.Permissions, permAdmins)
		}
	}
	return p
}

// canAddMembers mirrors GroupMembersUiState: the add button needs
// adminsCanAddMembers plus the permission for the kind of user being added.
func (p permissions) canAdd(g spondapi.Group, asAdmin bool) bool {
	if !g.AdminsCanAddMembers {
		return false
	}
	if asAdmin {
		return p.manageAdmins
	}
	return p.manageMembers
}

// memberKind describes how someone belongs to the group.
func memberKind(m spondapi.Membership) string {
	switch {
	case m.Respondent && m.IsAdmin() && m.PendingAdmin:
		return "Member, admin (invitation pending)"
	case m.Respondent && m.IsAdmin():
		return "Member and admin"
	case m.IsAdmin() && m.PendingAdmin:
		return "Admin (invitation pending)"
	case m.IsAdmin():
		return "Admin"
	case m.Respondent:
		return "Member"
	}
	return "Not a member"
}

// Change bodies for POST group/{id}/member. The app's
// EditableMembership.EditResult.toJsonElement sends the membership id plus
// only the fields that changed.

func subgroupsChange(membershipID string, subgroupIDs []string) map[string]any {
	ids := slices.Clone(subgroupIDs)
	slices.Sort(ids)
	if ids == nil {
		ids = []string{}
	}
	return map[string]any{"id": membershipID, "subGroups": ids}
}

func rolesChange(membershipID string, roleIDs []string) map[string]any {
	ids := slices.Clone(roleIDs)
	if ids == nil {
		ids = []string{}
	}
	return map[string]any{"id": membershipID, "roles": ids}
}

// respondentChange adds (AddMembershipAsMemberUseCase) or removes
// (RemoveMembershipAsMemberUseCase) member status.
func respondentChange(membershipID string, member bool) map[string]any {
	return map[string]any{"id": membershipID, "respondent": member}
}

// actor is who is about to change a membership, and where.
type actor struct {
	group  spondapi.Group
	groups []spondapi.Group // all loaded groups, for club contact-person checks
	me     *spondapi.MyProfile
	perms  permissions
}

func (a actor) isSelf(m spondapi.Membership) bool {
	return a.me != nil && m.Profile != nil && m.Profile.ID == a.me.ID
}

// contactPersonOf lists the groups where m is the contact person. For a club
// it also covers the club's own groups (GroupMemberActionsViewModel
// .resolveContactPersonGroupNames).
func (a actor) contactPersonOf(m spondapi.Membership) []string {
	if m.Profile == nil || m.Profile.ID == "" {
		return nil
	}
	is := func(g spondapi.Group) bool { return g.ContactPerson != nil && g.ContactPerson.ID == m.Profile.ID }
	names := []string{}
	if is(a.group) {
		names = append(names, a.group.Name)
	}
	if a.group.OrganizationType == "CLUB" {
		for _, g := range a.groups {
			if g.ID != a.group.ID && g.Club != nil && g.Club.ID == a.group.ID && is(g) {
				names = append(names, g.Name)
			}
		}
	}
	return names
}

var (
	errNoMemberRights = errors.New("you need the members permission in this group")
	errNoAdminRights  = errors.New("you need the admins permission in this group")
	errSelfRoles      = errors.New("you can't change your own admin roles here")
)

func (a actor) checkSubgroups(m spondapi.Membership) error {
	switch {
	case !a.perms.manageMembers:
		return errNoMemberRights
	case len(a.group.SubGroups) == 0:
		return errors.New("this group has no subgroups")
	}
	return nil
}

func (a actor) checkRoles(m spondapi.Membership, newRoles []string) error {
	switch {
	case !a.perms.manageAdmins:
		return errNoAdminRights
	case len(a.group.Roles) == 0:
		return errors.New("this group has no admin roles")
	case a.isSelf(m):
		return errSelfRoles
	case len(newRoles) == 0 && !m.Respondent:
		// RemoveMembershipAsAdminUseCase refuses this; the app deletes the
		// membership instead.
		return errors.New("they are only an admin; removing every role would leave nothing — remove them from the group instead")
	case len(newRoles) == 0 && len(a.contactPersonOf(m)) > 0:
		return fmt.Errorf("they are the contact person of %s; change the contact person in the Spond app first", strings.Join(a.contactPersonOf(m), ", "))
	}
	return nil
}

func (a actor) checkRespondent(m spondapi.Membership, member bool) error {
	switch {
	case !a.perms.manageMembers:
		return errNoMemberRights
	case member && m.Respondent:
		return errors.New("they are already a member")
	case !member && !m.Respondent:
		return errors.New("they are not a member")
	case !member && !m.IsAdmin():
		// RemoveMembershipAsMemberUseCase requires an admin role to keep.
		return errors.New("they are not an admin; remove them from the group instead")
	case !member && !a.perms.manageAdmins:
		return errNoAdminRights
	case a.isSelf(m):
		return errors.New("you can't change your own membership here")
	}
	return nil
}

func (a actor) checkDelete(m spondapi.Membership) error {
	switch {
	case !a.perms.manageMembers:
		return errNoMemberRights
	case m.IsAdmin() && !a.perms.manageAdmins:
		return errors.New("they are an admin; you need the admins permission to remove them")
	case a.isSelf(m):
		return errors.New("you can't remove yourself here; leave the group in the Spond app")
	}
	if groups := a.contactPersonOf(m); len(groups) > 0 {
		return fmt.Errorf("they are the contact person of %s; change the contact person in the Spond app first", strings.Join(groups, ", "))
	}
	return nil
}

// checkEdit: editing details needs the members permission (EditMemberViewModel);
// an admin's details are edited on the admin screen, which needs admins too.
func (a actor) checkEdit(m spondapi.Membership) error {
	switch {
	case !a.perms.manageMembers:
		return errNoMemberRights
	case m.IsAdmin() && !a.perms.manageAdmins:
		return errors.New("they are an admin; you need the admins permission to edit them")
	}
	return nil
}

func (a actor) checkGuardian(m spondapi.Membership) error {
	if !a.perms.manageMembers {
		return errNoMemberRights
	}
	return nil
}

// person is what the add-member and add-guardian forms collect.
type person struct {
	FirstName   string
	LastName    string
	Email       string
	PhoneNumber string
	DateOfBirth string
}

var (
	phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	phoneNoise   = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "")
)

// normalize trims and validates the fields. Phone numbers must be in
// international format (the app normalises them with the user's country,
// which a terminal client cannot know).
func (p person) normalize(requireName bool) (person, error) {
	p.FirstName = strings.TrimSpace(p.FirstName)
	p.LastName = strings.TrimSpace(p.LastName)
	p.Email = strings.ToLower(strings.TrimSpace(p.Email))
	p.PhoneNumber = phoneNoise.Replace(strings.TrimSpace(p.PhoneNumber))
	if strings.HasPrefix(p.PhoneNumber, "00") {
		p.PhoneNumber = "+" + p.PhoneNumber[2:]
	}
	p.DateOfBirth = strings.TrimSpace(p.DateOfBirth)

	switch {
	case requireName && (p.FirstName == "" || p.LastName == ""):
		return p, errors.New("first and last name are required")
	case p.Email != "" && (!strings.Contains(p.Email, "@") || strings.ContainsAny(p.Email, " ,;")):
		return p, fmt.Errorf("%q is not an email address", p.Email)
	case p.PhoneNumber != "" && !phonePattern.MatchString(p.PhoneNumber):
		return p, fmt.Errorf("%q is not an international phone number like +4712345678", p.PhoneNumber)
	}
	if p.DateOfBirth != "" {
		dob, err := time.Parse("2006-01-02", p.DateOfBirth)
		if err != nil {
			return p, fmt.Errorf("date of birth %q must be YYYY-MM-DD", p.DateOfBirth)
		}
		if dob.After(time.Now()) {
			return p, errors.New("date of birth is in the future")
		}
	}
	return p, nil
}

func (p person) guardian() spondapi.GuardianInput {
	return spondapi.GuardianInput{FirstName: p.FirstName, LastName: p.LastName, PhoneNumber: p.PhoneNumber, Email: p.Email}
}

// newMembershipBody is the body of POST group/{id}/member for a new member or
// admin, matching what UiMembershipFormKt.toEditableMembership produces for a
// new entity:
//
//   - firstName and lastName always; phoneNumber and email always, null when
//     empty (they are force-marked as changed);
//   - fields is always an empty custom-fields object;
//   - a member (MembershipSaveTarget.MEMBER) gets respondent: true, no roles;
//   - an admin (NEW_ADMIN) gets roles and no respondent (it stays false);
//   - subGroups, guardians, dateOfBirth only when set.
func newMembershipBody(p person, asAdmin bool, subgroupIDs, roleIDs []string, guardians []spondapi.GuardianInput) map[string]any {
	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	body := map[string]any{
		"firstName":   p.FirstName,
		"lastName":    p.LastName,
		"phoneNumber": nullable(p.PhoneNumber),
		"email":       nullable(p.Email),
		"fields":      map[string]any{},
	}
	if asAdmin {
		body["roles"] = slices.Clone(roleIDs)
	} else {
		body["respondent"] = true
	}
	if len(subgroupIDs) > 0 {
		ids := slices.Clone(subgroupIDs)
		slices.Sort(ids)
		body["subGroups"] = ids
	}
	if len(guardians) > 0 {
		body["guardians"] = guardians
	}
	if p.DateOfBirth != "" {
		body["dateOfBirth"] = p.DateOfBirth
	}
	return body
}

// memberSections lists everything known about a member, grouped like the
// arrangement modal.
func memberSections(m spondapi.Membership, g spondapi.Group) []section {
	subgroups := make(map[string]string, len(g.SubGroups))
	for _, sg := range g.SubGroups {
		subgroups[sg.ID] = sg.Name
	}
	roleNames := make(map[string]string, len(g.Roles))
	for i, r := range g.Roles {
		name := r.Name
		if i == 0 && name == "" {
			name = "Admin"
		}
		roleNames[r.ID] = name
	}

	card := ""
	if m.HasClubMembershipCard != nil {
		card = yesNo(*m.HasClubMembershipCard)
	}
	sections := []section{
		{"Membership", []field{
			{"Name", m.Name()},
			{"Type", memberKind(m)},
			{"Admin roles", strings.Join(roster.Lookup(m.Roles, roleNames), ", ")},
			{"Subgroups", strings.Join(roster.Lookup(m.SubGroups, subgroups), ", ")},
			{"Has the app", yesNo(m.Profile != nil)},
			{"Needs processing", yesNo(m.NeedsProcessing)},
			{"Added", display.Time(m.CreatedTime)},
		}},
		{"Contact", []field{
			{"Email", m.Email},
			{"Phone", m.PhoneNumber},
			{"Address", strings.Join(m.Address, "\n")},
		}},
		{"Personal", []field{
			{"Date of birth", m.DateOfBirth},
			{"Date of birth verified", yesNo(m.VerifiedDateOfBirth)},
			{"Gender", genderLabel(m.Gender)},
			{"Country", m.Country},
			{"Nationality", m.Nationality},
		}},
	}

	guardianFields := []field{}
	for i, gd := range m.Guardians {
		contact := []string{}
		for _, c := range []string{gd.Email, gd.PhoneNumber} {
			if c != "" {
				contact = append(contact, c)
			}
		}
		if gd.Profile != nil {
			contact = append(contact, "has the app")
		}
		value := gd.Name()
		if len(contact) > 0 {
			value += " (" + strings.Join(contact, ", ") + ")"
		}
		guardianFields = append(guardianFields, field{fmt.Sprintf("Guardian %d", i+1), value})
	}
	if len(guardianFields) == 0 {
		guardianFields = []field{{"Guardians", ""}}
	}
	sections = append(sections, section{"Guardians", guardianFields})

	if len(m.Fields) > 0 || len(g.FieldDefs) > 0 {
		defs := map[string]string{}
		custom := []field{}
		for _, d := range g.FieldDefs {
			defs[d.ID] = d.Name
			custom = append(custom, field{d.Name, fieldValue(m.Fields[d.ID])})
		}
		extra := []string{}
		for id := range m.Fields {
			if _, ok := defs[id]; !ok {
				extra = append(extra, id)
			}
		}
		slices.Sort(extra)
		for _, id := range extra {
			custom = append(custom, field{id, fieldValue(m.Fields[id])})
		}
		sections = append(sections, section{"Custom fields", custom})
	}

	p := m.Profile
	if p == nil {
		p = &spondapi.Profile{}
	}
	sections = append(sections,
		section{"App account", []field{
			{"Account name", strings.TrimSpace(p.FirstName + " " + p.LastName)},
			{"Account email", p.Email},
			{"Account phone", p.PhoneNumber},
			{"Contacted by", contactMethodLabel(p.ContactMethod)},
			{"Unreachable", yesNo(p.UnableToReach)},
		}},
		section{"Club", []field{
			{"Club membership id", m.ClubMembershipID},
			{"Club membership card", card},
		}},
		section{"Ids", []field{
			{"Membership id", m.ID},
			{"Profile id", p.ID},
			{"Image", m.ImageURL},
		}},
	)
	return sections
}

func fieldValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return fmt.Sprintf("%.0f", x)
	}
	return fmt.Sprint(v)
}

// genderLabel maps Gender's serial names (M, F, O).
func genderLabel(g string) string {
	switch g {
	case "M":
		return "Male"
	case "F":
		return "Female"
	case "O":
		return "Other"
	}
	return g
}

func contactMethodLabel(c string) string {
	switch c {
	case "app":
		return "App"
	case "email":
		return "Email"
	case "phone":
		return "Phone (SMS)"
	}
	return c
}
