package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/spondapi"
)

// nameIndex resolves the member, guardian and profile ids an arrangement
// refers to into display names.
type nameIndex map[string]string

func newNameIndex(s spondapi.Spond, g spondapi.Group) nameIndex {
	idx := nameIndex{}
	add := func(name string, ids ...string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		for _, id := range ids {
			if id != "" {
				if _, ok := idx[id]; !ok {
					idx[id] = name
				}
			}
		}
	}

	if r := s.Recipients; r != nil {
		if r.Group != nil {
			for _, m := range r.Group.Members {
				profileID := ""
				if m.Profile != nil {
					profileID = m.Profile.ID
				}
				add(m.FirstName+" "+m.LastName, m.ID, profileID)
			}
		}
		for _, p := range append(slices.Clone(r.Profiles), r.Guardians...) {
			add(p.FirstName+" "+p.LastName, p.ID, p.ProfileID)
		}
	}
	for _, m := range g.Members {
		profileID := ""
		if m.Profile != nil {
			profileID = m.Profile.ID
		}
		add(m.Name(), m.ID, profileID)
		for _, gd := range m.Guardians {
			guardianProfile := ""
			if gd.Profile != nil {
				guardianProfile = gd.Profile.ID
			}
			add(gd.Name(), gd.ID, guardianProfile)
		}
	}
	return idx
}

func (idx nameIndex) name(id string) string {
	if n, ok := idx[id]; ok {
		return n
	}
	return id
}

func (idx nameIndex) names(ids []string) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, idx.name(id))
	}
	return strings.Join(out, ", ")
}

// field is one labelled row of the detail view. An empty value renders as
// a dash.
type field struct {
	label string
	value string
}

type section struct {
	title  string
	fields []field
}

// detailSections lists every ApiSpond field with a readable label.
func detailSections(s spondapi.Spond, g spondapi.Group) []section {
	idx := newNameIndex(s, g)
	c := s.Counts()
	r := s.Responses
	if r == nil {
		r = &spondapi.ResponseBundle{}
	}

	loc := s.Location
	if loc == nil {
		loc = &spondapi.SpondLocation{}
	}
	coords := ""
	if loc.Latitude != nil && loc.Longitude != nil {
		coords = fmt.Sprintf("%.6f, %.6f", *loc.Latitude, *loc.Longitude)
	}

	meetup := ""
	if s.MeetupPrior > 0 {
		meetup = fmt.Sprintf("%s before start", display.Plural(s.MeetupPrior, "minute"))
		if t, err := time.Parse(time.RFC3339, s.StartTimestamp); err == nil {
			meetup += " (" + t.Add(-time.Duration(s.MeetupPrior)*time.Minute).Local().Format("15:04") + ")"
		}
	}

	hosts := make([]string, 0, len(s.Owners))
	for _, o := range s.Owners {
		hosts = append(hosts, fmt.Sprintf("%s (%s)", idx.name(o.ID), hostAnswer(o.Response)))
	}

	declines := []string{}
	for id, dm := range r.DeclineMessages {
		if dm.Message != "" {
			declines = append(declines, fmt.Sprintf("%s: %q", idx.name(id), dm.Message))
		}
	}
	slices.Sort(declines)

	attendance := []string{}
	for id, status := range r.RegisteredAttendance {
		attendance = append(attendance, idx.name(id)+": "+attendanceLabel(status))
	}
	slices.Sort(attendance)

	var group, groupType, subgroups, invitedProfiles, invitedGuardians string
	if rc := s.Recipients; rc != nil {
		if rc.Group != nil {
			group = rc.Group.Name
			groupType = groupTypeLabel(rc.Group.Type)
			names := make([]string, 0, len(rc.Group.SubGroups))
			for _, sg := range rc.Group.SubGroups {
				names = append(names, sg.Name)
			}
			subgroups = strings.Join(names, ", ")
		}
		invitedProfiles = recipientNames(rc.Profiles)
		invitedGuardians = recipientNames(rc.Guardians)
	}

	behalf := []string{}
	for _, id := range s.BehalfOfIDs {
		behalf = append(behalf, idx.name(id))
	}

	sections := []section{
		{"Overview", []field{
			{"Title", s.Heading},
			{"Type", spondTypeLabel(s.Type)},
			{"Starts", display.Time(s.StartTimestamp)},
			{"Ends", endLabel(s)},
			{"Meet-up", meetup},
			{"Answer deadline", display.Time(s.RsvpDate)},
			{"Description", strings.TrimSpace(s.Description)},
		}},
		{"Location", []field{
			{"Place", loc.Feature},
			{"Address", loc.Address},
			{"Postal code", loc.PostalCode},
			{"Locality", loc.Locality},
			{"Region", loc.AdministrativeAreaLevel1},
			{"Sub-region", loc.AdministrativeAreaLevel2},
			{"Country", loc.Country},
			{"Coordinates", coords},
		}},
		{"Status", []field{
			{"Cancelled", yesNo(s.Cancelled)},
			{"Cancellation reason", s.CancelledReason},
			{"Expired", yesNo(s.Expired)},
			{"Deleted", yesNo(s.Deleted)},
			{"Hidden", yesNo(s.Hidden)},
			{"Muted", yesNo(s.Muted)},
			{"Registered", yesNo(s.Registered)},
		}},
		{"Participation", []field{
			{"Attending", fmt.Sprint(c.AcceptedCount)},
			{"Not attending", fmt.Sprint(c.DeclinedCount)},
			{"Not answered", fmt.Sprint(c.UnansweredCount)},
			{"Waiting list", fmt.Sprint(c.WaitinglistCount)},
			{"Awaiting confirmation", fmt.Sprint(c.UnconfirmedCount)},
			{"Max participants", maxLabel(s.MaxAccepted)},
			{"Auto-accept", yesNo(s.AutoAccept)},
			{"Participant list hidden", yesNo(s.ParticipantsHidden)},
			{"Visible to", visibilityLabel(s.Visibility)},
		}},
		{"People", []field{
			{"Attending", idx.names(r.AcceptedIDs)},
			{"Not attending", idx.names(r.DeclinedIDs)},
			{"Not answered", idx.names(r.UnansweredIDs)},
			{"Waiting list", idx.names(r.WaitinglistIDs)},
			{"Awaiting confirmation", idx.names(r.UnconfirmedIDs)},
			{"Participants", idx.names(r.ParticipantIDs)},
			{"Decline messages", strings.Join(declines, "\n")},
			{"Registered attendance", strings.Join(attendance, "\n")},
			{"You answer for", strings.Join(behalf, ", ")},
		}},
		{"Organisation", []field{
			{"Created by", idx.name(s.CreatorID)},
			{"Hosts", strings.Join(hosts, ", ")},
			{"Group", group},
			{"Group type", groupType},
			{"Subgroups", subgroups},
			{"Invited profiles", invitedProfiles},
			{"Invited guardians", invitedGuardians},
			{"Invited member ids", strings.Join(nonNil(s.Recipients).GroupMembers, ", ")},
			{"Club", s.ClubName},
			{"Club id", s.ClubID},
			{"Reply-to email", s.ReplyToEmail},
		}},
		{"Payment", []field{
			{"Price", paymentLabel(s.Payment)},
		}},
		{"Reminders and invitations", []field{
			{"Automatic reminder", reminderLabel(s.AutoReminderType)},
			{"Reminder time", display.Time(s.AutoReminderTime)},
			{"Invitation time", display.Time(s.InviteTime)},
		}},
		{"Series", []field{
			{"Series id", s.SeriesID},
			{"Occurrence in series", ordinalLabel(s.SeriesOrdinal, s.SeriesID)},
			{"Changed from series", yesNo(s.ModifiedFromSeries)},
			{"Linked events", countLabel(len(nonNilChildren(s.Children).Events))},
		}},
	}

	if mi := s.MatchInfo; mi != nil {
		score := ""
		if mi.ScoresSet {
			score = fmt.Sprintf("%d – %d", mi.TeamScore, mi.OpponentScore)
		}
		sections = append(sections, section{"Match", []field{
			{"Match type", matchTypeLabel(mi.Type)},
			{"Team", mi.TeamName},
			{"Opponent", mi.OpponentName},
			{"Team colour", mi.TeamColour},
			{"Opponent colour", mi.OpponentColour},
			{"Score", score},
			{"Score entered", yesNo(mi.ScoresSet)},
			{"Score ever entered", yesNo(mi.ScoresSetEver)},
			{"Score public", yesNo(mi.ScoresPublic)},
			{"Score final", yesNo(mi.ScoresFinal)},
		}})
	}

	pm := s.PublicMetadata
	if pm == nil {
		pm = &spondapi.PublicMetadata{}
	}
	onBehalf := ""
	if p := s.MyPublicSignupOnBehalfOf; p != nil {
		onBehalf = strings.TrimSpace(p.FirstName + " " + p.LastName)
	}
	sections = append(sections,
		section{"Public event", []field{
			{"Share link", s.PublicEventShareURL},
			{"Activity", pm.ActivityType},
			{"Skill level", skillLabel(pm.SkillLevel)},
			{"Age group", pm.AgeGroup},
			{"My public signup", s.MyPublicSignupResponse},
			{"Signed up on behalf of", onBehalf},
			{"Signup group", s.SignupGroupName},
		}},
		section{"Other", []field{
			{"Comments", countLabel(len(s.Comments))},
			{"Comments disabled", yesNo(s.CommentsDisabled)},
			{"Attachments", countLabel(len(s.Attachments))},
			{"Open tasks", countLabel(len(nonNilTasks(s.Tasks).OpenTasks))},
			{"Assigned tasks", countLabel(len(nonNilTasks(s.Tasks).AssignedTasks))},
			{"Picture", s.Picture},
			{"Availability request id", s.AvailabilityRequestID},
			{"Created", display.Time(s.CreatedTime)},
			{"Last updated", epochMillisLabel(s.Updated)},
			{"Id", s.ID},
			{"App update needed", yesNo(s.ClientOutOfDate)},
		}},
	)
	return sections
}

func nonNil(r *spondapi.SpondRecipients) spondapi.SpondRecipients {
	if r == nil {
		return spondapi.SpondRecipients{}
	}
	return *r
}

func nonNilChildren(c *spondapi.SpondChildren) spondapi.SpondChildren {
	if c == nil {
		return spondapi.SpondChildren{}
	}
	return *c
}

func nonNilTasks(t *spondapi.SpondTasks) spondapi.SpondTasks {
	if t == nil {
		return spondapi.SpondTasks{}
	}
	return *t
}

func recipientNames(rs []spondapi.ProfileRecipient) string {
	names := make([]string, 0, len(rs))
	for _, p := range rs {
		names = append(names, strings.TrimSpace(p.FirstName+" "+p.LastName))
	}
	return strings.Join(names, ", ")
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func countLabel(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func maxLabel(n int) string {
	if n <= 0 {
		return "No limit"
	}
	return strconv.Itoa(n)
}

func ordinalLabel(n int, seriesID string) string {
	if seriesID == "" {
		return ""
	}
	return strconv.Itoa(n)
}

func endLabel(s spondapi.Spond) string {
	if s.OpenEnded {
		return "Open-ended"
	}
	return display.Time(s.EndTimestamp)
}

func epochMillisLabel(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Local().Format("Mon 2 Jan 2006 15:04")
}

// twoDecimalCurrencies are ISO 4217 currencies with 2 minor-unit digits that
// Spond is likely to use. Others are shown in raw minor units.
var twoDecimalCurrencies = []string{"NOK", "SEK", "DKK", "EUR", "GBP", "USD", "CHF", "PLN", "CAD", "AUD", "NZD"}

func paymentLabel(p *spondapi.SpondPayment) string {
	if p == nil || p.Total == 0 {
		return "Free"
	}
	if slices.Contains(twoDecimalCurrencies, strings.ToUpper(p.Currency)) {
		return fmt.Sprintf("%d.%02d %s", p.Total/100, p.Total%100, strings.ToUpper(p.Currency))
	}
	return fmt.Sprintf("%d %s (minor units)", p.Total, p.Currency)
}

func spondTypeLabel(t string) string {
	switch t {
	case "EVENT":
		return "Event"
	case "RECURRING":
		return "Recurring event"
	case "AVAILABILITY":
		return "Availability request"
	}
	return t
}

// visibilityLabel mirrors EventVisibility.fromProtocol: case-insensitive,
// anything other than ALL or PUBLIC means invitees only.
func visibilityLabel(v string) string {
	switch strings.ToUpper(v) {
	case "":
		return ""
	case "ALL":
		return "All group members"
	case "PUBLIC":
		return "Public"
	}
	return "Invitees only"
}

// groupTypeLabel maps GroupType's protocol int (GroupTypeSerializer).
func groupTypeLabel(code int) string {
	switch code {
	case 0:
		return ""
	case 1:
		return "Adults"
	case 2:
		return "Children"
	case 3:
		return "Mixed"
	}
	return strconv.Itoa(code)
}

func hostAnswer(r string) string {
	switch r {
	case "ACCEPTED":
		return "accepted"
	case "DECLINED":
		return "declined"
	case "UNANSWERED", "":
		return "not answered"
	}
	return strings.ToLower(r)
}

func attendanceLabel(a string) string {
	switch a {
	case "ATTENDED":
		return "Attended"
	case "ABSENT":
		return "Absent"
	case "VALID_ABSENCE":
		return "Valid absence"
	case "LATE":
		return "Late"
	}
	return a
}

func reminderLabel(r string) string {
	switch r {
	case "DISABLED":
		return "Off"
	case "REMIND_48H_BEFORE":
		return "48 hours before"
	case "REMIND_48H_AFTER":
		return "48 hours after"
	}
	return r
}

func matchTypeLabel(t string) string {
	switch t {
	case "HOME":
		return "Home"
	case "AWAY":
		return "Away"
	case "TOURNAMENT":
		return "Tournament"
	case "FRIENDLY":
		return "Friendly"
	}
	return t
}

func skillLabel(s string) string {
	switch s {
	case "BEGINNER":
		return "Beginner"
	case "INTERMEDIATE":
		return "Intermediate"
	case "ADVANCED":
		return "Advanced"
	case "ELITE":
		return "Elite"
	case "ALL_LEVELS":
		return "All levels"
	}
	return s
}
