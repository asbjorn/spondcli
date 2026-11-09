package spondapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
)

// captureQuery serves an empty JSON array at path and records each
// request's query.
func captureQuery(t *testing.T, path string) (*Client, *[]url.Values) {
	t.Helper()
	var got []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Errorf("path = %q, want %q", r.URL.Path, path)
		}
		got = append(got, r.URL.Query())
		w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.BaseURL = srv.URL + "/"
	return c, &got
}

func TestSpondsQuery(t *testing.T) {
	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC().Format(time.RFC3339)
	cursor := &Spond{ID: "S1", StartTimestamp: "2026-10-25T17:00:00.123456Z"}

	cases := []struct {
		name     string
		past     bool
		subgroup string
		cursor   *Spond
		want     url.Values
	}{
		{
			name: "upcoming first page",
			want: url.Values{
				"max": {"10"}, "includeComments": {"true"}, "groupId": {"G"}, "includeHidden": {"true"},
				"order": {"asc"}, "minEndTimestamp": {startOfToday},
			},
		},
		{
			name:   "upcoming next page",
			cursor: cursor,
			want: url.Values{
				"max": {"10"}, "includeComments": {"true"}, "groupId": {"G"}, "includeHidden": {"true"},
				"order": {"asc"}, "minEndTimestamp": {startOfToday},
				"minStartTimestamp": {"2026-10-25T17:00:00.123Z"}, "prevId": {"S1"},
			},
		},
		{
			name: "past first page",
			past: true,
			want: url.Values{
				"max": {"10"}, "includeComments": {"true"}, "groupId": {"G"}, "includeHidden": {"true"},
				"order": {"desc"}, "maxEndTimestamp": {startOfToday}, "excludeActive": {"true"},
			},
		},
		{
			name:   "past next page",
			past:   true,
			cursor: cursor,
			want: url.Values{
				"max": {"10"}, "includeComments": {"true"}, "groupId": {"G"}, "includeHidden": {"true"},
				"order": {"desc"}, "maxEndTimestamp": {startOfToday}, "excludeActive": {"true"},
				"maxStartTimestamp": {"2026-10-25T17:00:00.123Z"}, "prevId": {"S1"},
			},
		},
		{
			name:     "upcoming subgroup",
			subgroup: "SG",
			want: url.Values{
				"max": {"10"}, "includeComments": {"true"}, "groupId": {"G"}, "subGroupId": {"SG"},
				"includeHidden": {"true"}, "order": {"asc"}, "minEndTimestamp": {startOfToday},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, got := captureQuery(t, "/sponds")
			sc := Scope{GroupID: "G", SubgroupID: tc.subgroup}
			if _, err := c.Sponds(context.Background(), sc, tc.past, 10, tc.cursor); err != nil {
				t.Fatal(err)
			}
			if len(*got) != 1 || !reflect.DeepEqual((*got)[0], tc.want) {
				t.Errorf("query = %v\nwant    %v", *got, tc.want)
			}
		})
	}
}

func TestPostsQuery(t *testing.T) {
	cases := map[string]struct {
		sc   Scope
		want url.Values
	}{
		"group": {
			sc:   Scope{GroupID: "G"},
			want: url.Values{"type": {"PLAIN"}, "max": {"10"}, "includeComments": {"true"}, "groupId": {"G"}},
		},
		"subgroup": {
			sc: Scope{GroupID: "G", SubgroupID: "SG"},
			want: url.Values{
				"type": {"PLAIN"}, "max": {"10"}, "includeComments": {"true"}, "groupId": {"G"}, "subGroupId": {"SG"},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, got := captureQuery(t, "/posts")
			if _, err := c.Posts(context.Background(), tc.sc, 10, nil); err != nil {
				t.Fatal(err)
			}
			if len(*got) != 1 || !reflect.DeepEqual((*got)[0], tc.want) {
				t.Errorf("query = %v\nwant    %v", *got, tc.want)
			}
		})
	}
}

func TestInstantParam(t *testing.T) {
	cases := map[string]string{
		"2026-10-25T17:00:00Z":        "2026-10-25T17:00:00Z",
		"2026-10-25T17:00:00.000Z":    "2026-10-25T17:00:00Z",
		"2026-10-25T19:00:00.5+02:00": "2026-10-25T17:00:00.5Z",
		"not a time":                  "not a time",
	}
	for in, want := range cases {
		if got := instantParam(in); got != want {
			t.Errorf("instantParam(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSpondCounts(t *testing.T) {
	withCounts := Spond{ResponseCounts: &ResponseCounts{AcceptedCount: 3}, Responses: &ResponseBundle{AcceptedIDs: []string{"a"}}}
	if got := withCounts.Counts().AcceptedCount; got != 3 {
		t.Errorf("responseCounts should win: got %d, want 3", got)
	}
	fromIDs := Spond{Responses: &ResponseBundle{AcceptedIDs: []string{"a", "b"}, DeclinedIDs: []string{"c"}}}
	if c := fromIDs.Counts(); c.AcceptedCount != 2 || c.DeclinedCount != 1 {
		t.Errorf("fallback counts = %+v", c)
	}
	if c := (Spond{}).Counts(); c != (ResponseCounts{}) {
		t.Errorf("empty spond counts = %+v", c)
	}
}
