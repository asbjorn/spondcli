// Package spondapi is a client for the unofficial Spond API used by the
// Android app: login, groups, members, arrangements, posts, notifications
// and chat. See docs/API.md for the reverse-engineered endpoints.
package spondapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultBaseURL = "https://api.spond.com/core/v1/"

// Client talks to the Spond core API. Endpoints and payloads mirror the
// Android app; see docs/API.md.
type Client struct {
	http        *http.Client
	BaseURL     string
	Token       string
	SessionFile string       // where setToken caches the login; "" disables caching
	memberLimit atomic.Int64 // members page size; 0 means memberPageSize

	chatMu sync.Mutex
	Chat   ChatAuth // chat server login; empty until the first chat request
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}, BaseURL: defaultBaseURL}
}

type ExpiableToken struct {
	Token      string `json:"token"`
	Expiration string `json:"expiration"`
}

// LoginResponse covers both login response shapes: tokens, or a 2FA
// challenge carrying phoneNumber and token.
type LoginResponse struct {
	AccessToken *ExpiableToken `json:"accessToken"`
	PhoneNumber string         `json:"phoneNumber"`
	Token       string         `json:"token"`
}

type Group struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Activity            string        `json:"activity"`
	OrganizationType    string        `json:"organizationType"` // GROUP or CLUB
	Club                *GroupClub    `json:"club"`
	ContactPerson       *Profile      `json:"contactPerson"`
	AdminsCanAddMembers bool          `json:"adminsCanAddMembers"`
	SubGroups           []Subgroup    `json:"subGroups"`
	Roles               []Role        `json:"roles"` // roles[0] is the super-admin role
	FieldDefs           []FieldDef    `json:"fieldDefs"`
	Members             []Membership  `json:"members"`
	MyMembership        *Membership   `json:"myMembership"`
	MembershipRequests  []JoinRequest `json:"membershipRequests"`
	BehalfOfMemberships []Membership  `json:"behalfOfMemberships"`
}

// GroupClub is the club a group belongs to (ApiGroup.Club).
type GroupClub struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FieldDef is a custom member field definition (ApiCustomMemberFieldDef).
type FieldDef struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"` // TEXT, NUMBER, DATE, SINGLE_OPTION or MULTIPLE_OPTION
	Options    []string `json:"memberFieldOptions"`
	Permission string   `json:"permission"`
	Locked     bool     `json:"locked"`
	Required   bool     `json:"required"`
}

// JoinRequest is a pending request to join a group (ApiMembershipRequest).
// GroupMembershipID is set when the person claims an existing membership.
type JoinRequest struct {
	ID                string          `json:"id"`
	GroupMembershipID string          `json:"groupMembershipId"`
	Profile           *RequestPerson  `json:"profile"`
	Guardians         []RequestPerson `json:"guardians"`
	DateOfBirth       string          `json:"dateOfBirth"`
	Country           string          `json:"country"`
	Nationality       string          `json:"nationality"`
	Gender            string          `json:"gender"`
	Address           []string        `json:"address"`
	FieldValues       map[string]any  `json:"fieldValues"`
}

// RequestPerson is ApiMembershipRequest.Profile.
type RequestPerson struct {
	ID          string `json:"id"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	PhoneNumber string `json:"phoneNumber"`
	Email       string `json:"email"`
	Creator     bool   `json:"creator"`
}

func (p *RequestPerson) Name() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.FirstName + " " + p.LastName)
}

type Subgroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Role is an admin role (ApiGroupRole). Permissions use the wire names
// members, admins, settings, events, posts, polls, payments, chat, files and
// fundraisers.
type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// Profile is a Spond user account (ApiProfile). A membership links to one
// when the member has signed up to the app.
type Profile struct {
	ID            string `json:"id"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	Email         string `json:"email"`
	PhoneNumber   string `json:"phoneNumber"`
	ContactMethod string `json:"contactMethod"` // app, email or phone
	UnableToReach bool   `json:"unableToReach"`
}

// MyProfile is the logged-in user (ApiMyProfile).
type MyProfile struct {
	ID           string `json:"id"`
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	PrimaryEmail string `json:"primaryEmail"`
}

// Membership is a member of a group (ApiMembership). SubGroups and Roles
// hold ids that resolve against the owning group.
type Membership struct {
	ID                    string         `json:"id"`
	ClubMembershipID      string         `json:"clubMembershipId"`
	FirstName             string         `json:"firstName"`
	LastName              string         `json:"lastName"`
	Email                 string         `json:"email"`
	PhoneNumber           string         `json:"phoneNumber"`
	ImageURL              string         `json:"imageUrl"`
	DateOfBirth           string         `json:"dateOfBirth"`
	Gender                string         `json:"gender"` // M, F or O
	Country               string         `json:"country"`
	Nationality           string         `json:"nationality"`
	CreatedTime           string         `json:"createdTime"`
	Address               []string       `json:"address"`
	Fields                map[string]any `json:"fields"`     // custom field id -> string or number
	Respondent            bool           `json:"respondent"` // is a member (not only an admin)
	PendingAdmin          bool           `json:"pendingAdmin"`
	NeedsProcessing       bool           `json:"needsProcessing"`
	VerifiedDateOfBirth   bool           `json:"verifiedDateOfBirth"`
	HasClubMembershipCard *bool          `json:"hasClubMembershipCard"`
	SubGroups             []string       `json:"subGroups"`
	Roles                 []string       `json:"roles"`
	Guardians             []Guardian     `json:"guardians"`
	Profile               *Profile       `json:"profile"`
}

type Guardian struct {
	ID          string   `json:"id"`
	FirstName   string   `json:"firstName"`
	LastName    string   `json:"lastName"`
	Email       string   `json:"email"`
	PhoneNumber string   `json:"phoneNumber"`
	Profile     *Profile `json:"profile"`
}

func (m Membership) Name() string {
	return strings.TrimSpace(m.FirstName + " " + m.LastName)
}

func (g Guardian) Name() string {
	return strings.TrimSpace(g.FirstName + " " + g.LastName)
}

type MembersPage struct {
	Members    []Membership `json:"members"`
	NextCursor string       `json:"nextCursor"`
}

type Post struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Timestamp   string    `json:"timestamp"`
	OwnerID     string    `json:"ownerId"`
	GroupID     string    `json:"groupId"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	SubGroupIDs []string  `json:"subGroupIds"`
	Comments    []Comment `json:"comments,omitempty"` // only with includeComments=true
}

// Comment is a post comment (ApiComment). Children are its replies, one
// level deep. The API omits "edited" when false and has no edit time.
type Comment struct {
	ID            string    `json:"id"`
	FromProfileID string    `json:"fromProfileId"`
	Timestamp     string    `json:"timestamp"`
	Text          string    `json:"text"`
	Edited        bool      `json:"edited,omitempty"`
	Children      []Comment `json:"children,omitempty"`
}

// APIError is returned for non-2xx responses.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("spond API: HTTP %d: %s", e.Status, e.Body)
}

// errorKey returns the "errorKey" of a Spond error body, such as
// "inviteNotSent", or "" when the body has none.
func (e *APIError) errorKey() string {
	var body struct {
		ErrorKey string `json:"errorKey"`
	}
	if json.Unmarshal([]byte(e.Body), &body) != nil {
		return ""
	}
	return body.ErrorKey
}

// HasErrorKey reports whether err is a Spond API error with the given key.
func HasErrorKey(err error, key string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.errorKey() == key
}

// Login returns the access token, or an empty token plus a 2FA challenge token.
func (c *Client) Login(ctx context.Context, email, password string) (*LoginResponse, error) {
	body := map[string]string{"email": email, "password": password}
	var resp LoginResponse
	if err := c.do(ctx, http.MethodPost, "auth2/login", nil, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Verify2FA(ctx context.Context, challengeToken, code string) (*ExpiableToken, error) {
	body := map[string]string{"token": challengeToken, "code": code}
	var resp LoginResponse
	if err := c.do(ctx, http.MethodPost, "auth2/2fa/verify", nil, body, &resp); err != nil {
		return nil, err
	}
	if resp.AccessToken == nil {
		return nil, fmt.Errorf("2FA verify: response has no accessToken")
	}
	return resp.AccessToken, nil
}

func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	query := url.Values{"includeMembers": {"true"}}
	groups := []Group{}
	if err := c.do(ctx, http.MethodGet, "groups", query, nil, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (c *Client) Me(ctx context.Context) (*MyProfile, error) {
	var p MyProfile
	if err := c.do(ctx, http.MethodGet, "profile", nil, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Members page sizes, as in GroupRepositoryImpl.syncGroupMembers: the app asks
// for 2000 members per page and, if the server rejects that with HTTP 400,
// drops to 500 for the rest of the session.
const (
	memberPageSize         = 2000
	memberPageSizeFallback = 500
)

// MembersPage fetches one page of a group's members; cursor is "" for the
// first page.
func (c *Client) MembersPage(ctx context.Context, groupID, cursor string) (*MembersPage, error) {
	for {
		limit := int(c.memberLimit.Load())
		if limit == 0 {
			limit = memberPageSize
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page MembersPage
		err := c.do(ctx, http.MethodGet, "group/"+url.PathEscape(groupID)+"/members", query, nil, &page)
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusBadRequest && limit > memberPageSizeFallback {
			c.memberLimit.Store(memberPageSizeFallback)
			continue
		}
		if err != nil {
			return nil, err
		}
		return &page, nil
	}
}

// NextMembersCursor returns the cursor for the next page, or "" when done.
// Like the app it follows nextCursor until there is none; a cursor that
// repeats is treated as the end so a misbehaving server cannot loop forever.
func NextMembersCursor(page *MembersPage, current string) string {
	if page.NextCursor == current {
		return ""
	}
	return page.NextCursor
}

// MergeMembers adds a page to the list, replacing members already present
// (the app upserts by id).
func MergeMembers(list, page []Membership) []Membership {
	index := make(map[string]int, len(list))
	for i, m := range list {
		index[m.ID] = i
	}
	for _, m := range page {
		if i, ok := index[m.ID]; ok {
			list[i] = m
			continue
		}
		index[m.ID] = len(list)
		list = append(list, m)
	}
	return list
}

// Members fetches every member of a group, following nextCursor.
func (c *Client) Members(ctx context.Context, groupID string) ([]Membership, error) {
	members := []Membership{}
	cursor := ""
	for {
		page, err := c.MembersPage(ctx, groupID, cursor)
		if err != nil {
			return nil, err
		}
		members = MergeMembers(members, page.Members)
		if cursor = NextMembersCursor(page, cursor); cursor == "" {
			return members, nil
		}
	}
}

// SaveMembership creates or updates one membership (GroupApi.addOrUpdateMembership):
// POST group/{groupId}/member with the change body built by membershipChange.
func (c *Client) SaveMembership(ctx context.Context, groupID string, body map[string]any) (*Membership, error) {
	var m Membership
	if err := c.do(ctx, http.MethodPost, "group/"+url.PathEscape(groupID)+"/member", nil, body, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// DeleteMembership removes a membership from the group entirely
// (GroupApi.deleteMembership): DELETE group/{groupId}/members/{membershipId}.
func (c *Client) DeleteMembership(ctx context.Context, groupID, membershipID string) error {
	path := "group/" + url.PathEscape(groupID) + "/members/" + url.PathEscape(membershipID)
	return c.do(ctx, http.MethodDelete, path, nil, nil, nil)
}

// ApproveRequest accepts a join request into the given subgroups
// (GroupApi.approveMembershipRequest):
// POST group/{groupId}/membershipRequest/{requestId}/approve {"subGroups": [...]}.
func (c *Client) ApproveRequest(ctx context.Context, groupID, requestID string, subgroupIDs []string) (*Membership, error) {
	ids := slices.Clone(subgroupIDs)
	if ids == nil {
		ids = []string{}
	}
	var m Membership
	path := "group/" + url.PathEscape(groupID) + "/membershipRequest/" + url.PathEscape(requestID) + "/approve"
	if err := c.do(ctx, http.MethodPost, path, nil, map[string]any{"subGroups": ids}, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// RejectRequest declines a join request (GroupApi.rejectMembershipRequest):
// POST group/{groupId}/membershipRequest/{requestId}/reject, no body.
func (c *Client) RejectRequest(ctx context.Context, groupID, requestID string) error {
	path := "group/" + url.PathEscape(groupID) + "/membershipRequest/" + url.PathEscape(requestID) + "/reject"
	return c.do(ctx, http.MethodPost, path, nil, nil, nil)
}

// AddGuardian adds a guardian to a member (GroupApi.addGuardian):
// POST group/{groupId}/member/{membershipId}/guardian.
func (c *Client) AddGuardian(ctx context.Context, groupID, membershipID string, g GuardianInput) (*Membership, error) {
	var m Membership
	path := "group/" + url.PathEscape(groupID) + "/member/" + url.PathEscape(membershipID) + "/guardian"
	if err := c.do(ctx, http.MethodPost, path, nil, g, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Scope selects a whole group, or one subgroup of it when subgroupID is set.
// The app sends groupId together with subGroupId for subgroup feeds.
type Scope struct {
	GroupID    string
	SubgroupID string
}

// apply adds the scope to a query. The zero scope means every group, as on
// the app's home screen.
func (s Scope) apply(query url.Values) {
	if s.GroupID == "" {
		return
	}
	query.Set("groupId", s.GroupID)
	if s.SubgroupID != "" {
		query.Set("subGroupId", s.SubgroupID)
	}
}

// Sponds fetches one page of a group's arrangements (every group's for the
// zero scope, as on the app's home screen), mirroring the app's
// SpondRepositoryImpl.PagingSyncAdapter with the default filter:
//
//   - upcoming: ascending, events ending today or later, next page starts at
//     the cursor's start time;
//   - past: descending, events that ended before today and are not ongoing,
//     next page starts before the cursor's start time.
//
// "Today" is local midnight. cursor is the last arrangement of the previous
// page, or nil for the first page.
func (c *Client) Sponds(ctx context.Context, sc Scope, past bool, max int, cursor *Spond) ([]Spond, error) {
	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC().Format(time.RFC3339)

	query := url.Values{
		"max":             {strconv.Itoa(max)},
		"includeComments": {"true"},
	}
	if sc.GroupID != "" {
		query.Set("includeHidden", "true") // the app sets it whenever a group is given
	}
	sc.apply(query)
	switch {
	case past:
		query.Set("order", "desc")
		query.Set("maxEndTimestamp", startOfToday)
		query.Set("excludeActive", "true")
		if cursor != nil {
			query.Set("maxStartTimestamp", instantParam(cursor.StartTimestamp))
		}
	default:
		query.Set("order", "asc")
		query.Set("minEndTimestamp", startOfToday)
		if cursor != nil {
			query.Set("minStartTimestamp", instantParam(cursor.StartTimestamp))
		}
	}
	if cursor != nil {
		query.Set("prevId", cursor.ID)
	}

	sponds := []Spond{}
	if err := c.do(ctx, http.MethodGet, "sponds", query, nil, &sponds); err != nil {
		return nil, err
	}
	return sponds, nil
}

// Spond fetches one arrangement with comments (SpondApi.getSpond).
func (c *Client) Spond(ctx context.Context, id string) (*Spond, error) {
	query := url.Values{"includeComments": {"true"}}
	var s Spond
	if err := c.do(ctx, http.MethodGet, "sponds/"+url.PathEscape(id), query, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Respond answers an arrangement for one recipient (SpondApi.respondToSpond):
// PUT sponds/{id}/responses/{recipientId} with {"accepted": bool} and, only
// when declining with a non-empty message, "declineMessage". It returns the
// updated response lists.
//
// The app never uses this to accept an arrangement with a price; it runs a
// payment flow instead (see spond.requiresPayment).
func (c *Client) Respond(ctx context.Context, spondID, recipientID string, accepted bool, declineMsg string) (*ResponseBundle, error) {
	body := map[string]any{"accepted": accepted}
	if !accepted && declineMsg != "" {
		body["declineMessage"] = declineMsg
	}
	var bundle ResponseBundle
	path := "sponds/" + url.PathEscape(spondID) + "/responses/" + url.PathEscape(recipientID)
	if err := c.do(ctx, http.MethodPut, path, nil, body, &bundle); err != nil {
		return nil, err
	}
	return &bundle, nil
}

// instantParam formats a timestamp the way the app does: UTC, truncated to
// milliseconds (the app keeps the cursor as epoch millis). Unparseable input
// is passed through unchanged.
func instantParam(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return t.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)
}

// Posts fetches one page of plain posts with comments for a group or
// subgroup, older than the cursor post (if non-nil).
func (c *Client) Posts(ctx context.Context, sc Scope, max int, cursor *Post) ([]Post, error) {
	return c.postsPage(ctx, sc, max, cursor, true)
}

// SyncPageSize is the page size for full downloads. A large group's whole
// history (about 220 posts with comments) fits in one page of 500.
const SyncPageSize = 500

// AllPosts fetches every plain post of a group or subgroup, with comments,
// newest first.
//
// A group's feed leaves out posts sent to subgroups, and a subgroup's feed
// holds only those, so a complete download of a group needs both (see
// syncPosts).
func (c *Client) AllPosts(ctx context.Context, sc Scope) ([]Post, error) {
	var all []Post
	var cursor *Post
	for {
		page, err := c.postsPage(ctx, sc, SyncPageSize, cursor, true)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < SyncPageSize || page[len(page)-1].ID == cursorID(cursor) {
			return all, nil
		}
		cursor = &page[len(page)-1]
	}
}

func cursorID(p *Post) string {
	if p == nil {
		return ""
	}
	return p.ID
}

func (c *Client) postsPage(ctx context.Context, sc Scope, max int, cursor *Post, withComments bool) ([]Post, error) {
	query := url.Values{
		"type":            {"PLAIN"},
		"max":             {strconv.Itoa(max)},
		"includeComments": {strconv.FormatBool(withComments)},
	}
	sc.apply(query)
	if cursor != nil {
		query.Set("maxTimestamp", cursor.Timestamp)
		query.Set("prevId", cursor.ID)
	}
	posts := []Post{}
	if err := c.do(ctx, http.MethodGet, "posts", query, nil, &posts); err != nil {
		return nil, err
	}
	return posts, nil
}

func (c *Client) do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body any,
	out any,
) error {
	header := http.Header{}
	if c.Token != "" {
		header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.send(ctx, method, c.BaseURL+path, query, header, body, out)
}

// send makes one JSON request to an absolute URL with extra headers.
func (c *Client) send(
	ctx context.Context,
	method string,
	u string,
	query url.Values,
	header http.Header,
	body any,
	out any,
) error {
	path := strings.TrimPrefix(u, c.BaseURL) // short name for errors
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Spond-Timezone", time.Local.String())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading %s response: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Status: resp.StatusCode, Body: CleanText(string(data))}
	}
	if out == nil {
		return nil // caller does not need the body (e.g. DELETE)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	Sanitize(out)
	return nil
}

// GuardianInput is the body of POST group/{id}/member/{id}/guardian
// (EditedGuardian). The app's JSON omits nulls (explicitNulls = false).
type GuardianInput struct {
	FirstName   string `json:"firstName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
	Email       string `json:"email,omitempty"`
}

func (g GuardianInput) Empty() bool {
	return g.FirstName == "" && g.LastName == "" && g.PhoneNumber == "" && g.Email == ""
}

func (m Membership) IsAdmin() bool { return len(m.Roles) > 0 }
