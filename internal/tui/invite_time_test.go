package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

func TestInvitationsSent(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := map[string]bool{
		"":                     true, // no schedule
		"2026-10-22T11:00:00Z": true, // already sent
		"2026-10-22T12:00:00Z": true, // exactly now counts as sent (<=)
		"2026-10-23T08:00:00Z": false,
	}
	for invite, want := range cases {
		if got := (spondapi.Spond{InviteTime: invite}).InvitationsSent(now); got != want {
			t.Errorf("inviteTime %q: sent = %v, want %v", invite, got, want)
		}
	}
}

func TestScheduledArrangementCannotBeAnswered(t *testing.T) {
	c, calls := fakeSpond(t, spondapi.Spond{ID: "S1"})
	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	s := spondapi.Spond{ID: "S1", Heading: "Cup", BehalfOfIDs: []string{"M1"}, InviteTime: future,
		Responses: &spondapi.ResponseBundle{UnansweredIDs: []string{"M1"}}}
	m := eventsModel(t, c, s)

	if !strings.Contains(m.View(), "Invitations scheduled") {
		t.Errorf("card should show that invitations are scheduled")
	}
	m = send(m, key("enter"))
	view := m.View()
	if !strings.Contains(view, "haven't been sent yet") || strings.Contains(view, "[y] Attend") {
		t.Errorf("modal should explain the schedule instead of offering answers")
	}
	m = send(m, key("y"))
	if m.err == nil || !strings.Contains(m.err.Error(), "They go out on") {
		t.Errorf("y: err = %v", m.err)
	}
	if len(calls()) != 0 {
		t.Errorf("a scheduled arrangement must not be answered on the server")
	}
}

func TestInviteNotSentErrorIsFriendly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"Invitation has not been sent out","errorCode":3004,"errorKey":"inviteNotSent"}`))
	}))
	t.Cleanup(srv.Close)
	c := spondapi.NewClient()
	c.BaseURL = srv.URL + "/"

	_, err := c.Respond(context.Background(), "S1", "M1", true, "")
	if !spondapi.HasErrorKey(err, "inviteNotSent") {
		t.Fatalf("errorKey not recognised: %v", err)
	}

	// The server can still refuse if the local clock is ahead of it.
	s := spondapi.Spond{ID: "S1", BehalfOfIDs: []string{"M1"}, Responses: &spondapi.ResponseBundle{UnansweredIDs: []string{"M1"}}}
	m := eventsModel(t, c, s)
	m = send(m, key("enter"))
	m = send(m, key("y"))
	if m.err == nil || strings.Contains(m.err.Error(), "HTTP 400") || !strings.Contains(m.err.Error(), "haven't been sent yet") {
		t.Errorf("server inviteNotSent should be shown in plain words, got %v", m.err)
	}
}
