package spondapi

import (
	"slices"
	"time"
)

// Spond is an arrangement/event (ApiSpond), decoded in full so the detail
// view can show every field.
type Spond struct {
	ID                       string           `json:"id"`
	Type                     string           `json:"type"` // EVENT, RECURRING or AVAILABILITY
	Heading                  string           `json:"heading"`
	Description              string           `json:"description"`
	StartTimestamp           string           `json:"startTimestamp"`
	EndTimestamp             string           `json:"endTimestamp"`
	OpenEnded                bool             `json:"openEnded"`
	MeetupPrior              int              `json:"meetupPrior"` // minutes before start
	RsvpDate                 string           `json:"rsvpDate"`
	Location                 *SpondLocation   `json:"location"`
	CreatorID                string           `json:"creatorId"`
	AvailabilityRequestID    string           `json:"availabilityRequestId"`
	Owners                   []spondOwner     `json:"owners"`
	BehalfOfIDs              []string         `json:"behalfOfIds"`
	Recipients               *SpondRecipients `json:"recipients"`
	Responses                *ResponseBundle  `json:"responses"`
	ResponseCounts           *ResponseCounts  `json:"responseCounts"`
	Deleted                  bool             `json:"deleted"`
	Cancelled                bool             `json:"cancelled"`
	CancelledReason          string           `json:"cancelledReason"`
	Hidden                   bool             `json:"hidden"`
	Updated                  int64            `json:"updated"`
	Picture                  string           `json:"picture"`
	CreatedTime              string           `json:"createdTime"`
	Expired                  bool             `json:"expired"`
	Muted                    bool             `json:"muted"`
	ClientOutOfDate          bool             `json:"clientOutOfDate"`
	SeriesID                 string           `json:"seriesId"`
	InviteTime               string           `json:"inviteTime"`
	SeriesOrdinal            int              `json:"seriesOrdinal"`
	ModifiedFromSeries       bool             `json:"modifiedFromSeries"`
	MaxAccepted              int              `json:"maxAccepted"`
	Payment                  *SpondPayment    `json:"payment"`
	AutoAccept               bool             `json:"autoAccept"`
	ParticipantsHidden       bool             `json:"participantsHidden"`
	AutoReminderType         string           `json:"autoReminderType"`
	AutoReminderTime         string           `json:"autoReminderTime"`
	Registered               bool             `json:"registered"`
	Visibility               string           `json:"visibility"`
	CommentsDisabled         bool             `json:"commentsDisabled"`
	MyPublicSignupResponse   string           `json:"myPublicSignupResponse"`
	MyPublicSignupOnBehalfOf *personName      `json:"myPublicSignupOnBehalfOf"`
	SignupGroupName          string           `json:"groupName"`
	PublicMetadata           *PublicMetadata  `json:"publicMetadata"`
	PublicEventShareURL      string           `json:"publicEventShareUrl"`
	Children                 *SpondChildren   `json:"children"`
	Tasks                    *SpondTasks      `json:"tasks"`
	ClubID                   string           `json:"clubId"`
	ClubName                 string           `json:"clubName"`
	ReplyToEmail             string           `json:"replyToEmail"`
	MatchInfo                *matchInfo       `json:"matchInfo"`
	Attachments              []map[string]any `json:"attachments"`
	Comments                 []map[string]any `json:"comments"`
}

type SpondLocation struct {
	ID                       string   `json:"id"`
	Feature                  string   `json:"feature"`
	Address                  string   `json:"address"`
	PostalCode               string   `json:"postalCode"`
	Country                  string   `json:"country"`
	AdministrativeAreaLevel1 string   `json:"administrativeAreaLevel1"`
	AdministrativeAreaLevel2 string   `json:"administrativeAreaLevel2"`
	Locality                 string   `json:"locality"`
	Longitude                *float64 `json:"longitude"`
	Latitude                 *float64 `json:"latitude"`
}

// spondOwner is a host; id is a profile id.
type spondOwner struct {
	ID       string `json:"id"`
	Response string `json:"response"` // ACCEPTED, DECLINED or UNANSWERED
}

type SpondRecipients struct {
	Group        *RecipientGroup    `json:"group"`
	Profiles     []ProfileRecipient `json:"profiles"`
	Guardians    []ProfileRecipient `json:"guardians"`
	GroupMembers []string           `json:"groupMembers"`
}

type RecipientGroup struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      int               `json:"type"` // GroupType wire code: 1 adults, 2 children, 3 mixed
	Activity  string            `json:"activity"`
	SubGroups []Subgroup        `json:"subGroups"`
	Members   []recipientMember `json:"members"`
}

type recipientMember struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Profile   *struct {
		ID string `json:"id"`
	} `json:"profile"`
}

type ProfileRecipient struct {
	ID          string `json:"id"`
	ProfileID   string `json:"profileId"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	PhoneNumber string `json:"phoneNumber"`
	Email       string `json:"email"`
}

type personName struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// ResponseBundle lists recipient ids per answer. PUT
// sponds/{id}/responses/{recipientId} returns an updated copy.
type ResponseBundle struct {
	AcceptedIDs          []string                  `json:"acceptedIds"`
	DeclinedIDs          []string                  `json:"declinedIds"`
	UnansweredIDs        []string                  `json:"unansweredIds"`
	WaitinglistIDs       []string                  `json:"waitinglistIds"`
	ParticipantIDs       []string                  `json:"participantIds"`
	UnconfirmedIDs       []string                  `json:"unconfirmedIds"`
	DeclineMessages      map[string]declineMessage `json:"declineMessages"`
	RegisteredAttendance map[string]string         `json:"registeredAttendance"`
}

type declineMessage struct {
	ProfileID string `json:"profileId"`
	Message   string `json:"message"`
}

type ResponseCounts struct {
	AcceptedCount    int `json:"acceptedCount"`
	DeclinedCount    int `json:"declinedCount"`
	UnansweredCount  int `json:"unansweredCount"`
	WaitinglistCount int `json:"waitinglistCount"`
	UnconfirmedCount int `json:"unconfirmedCount"`
}

// SpondPayment holds the price in minor units of the currency.
type SpondPayment struct {
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
}

type PublicMetadata struct {
	ActivityType string `json:"activityType"`
	SkillLevel   string `json:"skillLevel"`
	AgeGroup     string `json:"ageGroup"`
}

type SpondChildren struct {
	Events []string `json:"events"`
}

type SpondTasks struct {
	OpenTasks     []map[string]any `json:"openTasks"`
	AssignedTasks []map[string]any `json:"assignedTasks"`
}

type matchInfo struct {
	Type           string `json:"type"` // HOME, AWAY, TOURNAMENT or FRIENDLY
	TeamName       string `json:"teamName"`
	OpponentName   string `json:"opponentName"`
	TeamColour     string `json:"teamColour"`
	OpponentColour string `json:"opponentColour"`
	ScoresSet      bool   `json:"scoresSet"`
	ScoresPublic   bool   `json:"scoresPublic"`
	ScoresFinal    bool   `json:"scoresFinal"`
	ScoresSetEver  bool   `json:"scoresSetEver"`
	TeamScore      int    `json:"teamScore"`
	OpponentScore  int    `json:"opponentScore"`
}

// Counts prefers the server's responseCounts and falls back to the id lists.
func (s Spond) Counts() ResponseCounts {
	if s.ResponseCounts != nil {
		return *s.ResponseCounts
	}
	if s.Responses == nil {
		return ResponseCounts{}
	}
	r := s.Responses
	return ResponseCounts{
		AcceptedCount:    len(r.AcceptedIDs),
		DeclinedCount:    len(r.DeclinedIDs),
		UnansweredCount:  len(r.UnansweredIDs),
		WaitinglistCount: len(r.WaitinglistIDs),
		UnconfirmedCount: len(r.UnconfirmedIDs),
	}
}

// InvitationsSent mirrors Spond.isInvited: invitations go out at inviteTime,
// and until then nobody can answer (the server replies "inviteNotSent").
func (s Spond) InvitationsSent(now time.Time) bool {
	if s.InviteTime == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, s.InviteTime)
	return err != nil || !t.After(now)
}

// InvitationsPending explains when invitations go out.
func (s Spond) InvitationsPending() string {
	when := "later"
	if t, err := time.Parse(time.RFC3339, s.InviteTime); err == nil {
		when = "on " + t.Local().Format("Mon 2 Jan 2006 at 15:04")
	}
	return "Invitations for this arrangement haven't been sent yet. They go out " + when + "; you can answer after that."
}

// RequiresPayment reports whether joining goes through the app's payment
// flow (JoinPaymentSpondUseCase) instead of a plain response.
func (s Spond) RequiresPayment() bool {
	return s.Payment != nil && s.Payment.Total > 0
}

// Answer is a recipient's response to an arrangement.
type Answer int

const (
	AnswerNone        Answer = iota // not among the recipients
	AnswerUnanswered                // invited, no answer yet
	AnswerAccepted                  // attending
	AnswerUnconfirmed               // accepted, awaiting confirmation
	AnswerWaiting                   // on the waiting list
	AnswerDeclined                  // not attending
)

func (a Answer) String() string {
	switch a {
	case AnswerUnanswered:
		return "Not answered"
	case AnswerAccepted:
		return "Attending"
	case AnswerUnconfirmed:
		return "Attending (awaiting confirmation)"
	case AnswerWaiting:
		return "On waiting list"
	case AnswerDeclined:
		return "Not attending"
	}
	return "Not invited"
}

// AnswerFor mirrors SpondMapperKt.calcResponseType for a single recipient,
// including its precedence: unconfirmed, unanswered, accepted, waiting list,
// declined.
func (r *ResponseBundle) AnswerFor(id string) Answer {
	if r == nil || id == "" {
		return AnswerNone
	}
	switch {
	case slices.Contains(r.UnconfirmedIDs, id):
		return AnswerUnconfirmed
	case slices.Contains(r.UnansweredIDs, id):
		return AnswerUnanswered
	case slices.Contains(r.AcceptedIDs, id):
		return AnswerAccepted
	case slices.Contains(r.WaitinglistIDs, id):
		return AnswerWaiting
	case slices.Contains(r.DeclinedIDs, id):
		return AnswerDeclined
	}
	return AnswerNone
}
