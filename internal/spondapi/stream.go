package spondapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
)

// streamPageSize is the app's page size for SyncNewActivities.
const streamPageSize = 100

// maxStreamPages bounds one poll after a long sleep; the cursor still moves,
// so the next poll goes on from there.
const maxStreamPages = 10

// StreamActivity is one raw event of the activity stream (ApiActivity), as
// opposed to the grouped notifications in activity. Type is lower case
// (new_post, update_spond, post_comment, …).
type StreamActivity struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Spond     *IDRef `json:"spond"`
	Post      *IDRef `json:"post"`
	Group     *IDRef `json:"group"`
	Chat      *IDRef `json:"chat"`
}

type IDRef struct {
	ID string `json:"id"`
}

// StreamCursor is the newest activity seen (the app's ActivitySyncCursor).
type StreamCursor struct {
	Timestamp string
	ID        string
}

// NewestActivity returns the newest activity, or nil when there is none.
func (c *Client) NewestActivity(ctx context.Context) (*StreamActivity, error) {
	query := url.Values{"order": {"desc"}, "max": {"1"}}
	items := []StreamActivity{}
	if err := c.do(ctx, http.MethodGet, "activities", query, nil, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}

// ActivitiesSince returns the activities after cur, oldest first, and the
// cursor to continue from. The server only accepts since with order=asc, and
// the bound is inclusive, so the cursor's own activity comes back and is
// dropped (verified live, 2026-10).
func (c *Client) ActivitiesSince(ctx context.Context, cur StreamCursor) ([]StreamActivity, StreamCursor, error) {
	var out []StreamActivity
	for range maxStreamPages {
		query := url.Values{"order": {"asc"}, "since": {cur.Timestamp}, "max": {strconv.Itoa(streamPageSize)}}
		page := []StreamActivity{}
		if err := c.do(ctx, http.MethodGet, "activities", query, nil, &page); err != nil {
			return out, cur, err
		}
		fresh := slices.DeleteFunc(slices.Clone(page), func(a StreamActivity) bool { return a.ID == cur.ID })
		out = append(out, fresh...)
		if len(fresh) == 0 {
			break
		}
		last := fresh[len(fresh)-1]
		cur = StreamCursor{Timestamp: last.Timestamp, ID: last.ID}
		if len(page) < streamPageSize {
			break
		}
	}
	return out, cur, nil
}

func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}
