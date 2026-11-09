// Package store is the local SQLite cache of group posts and its
// full-text search.
package store

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite" // pure Go, so builds for Windows, macOS and Linux need no C compiler

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// Store is the local post cache: an SQLite database with a full-text
// index over titles, bodies and comments.
//
// The API offers no way to ask whether a post changed (no ETag or
// Last-Modified, no update time on posts or comments), so a sync downloads
// a group's whole history and keeps a hash per post to skip re-indexing
// posts that did not change.
type Store struct {
	db *sql.DB
}

// storeSchemaVersion is stored as PRAGMA user_version. The database is only
// a cache, so a version mismatch drops and recreates it.
const storeSchemaVersion = 3

// syncMaxAge is how long a group's cached posts are searched without
// syncing first.
const syncMaxAge = 15 * time.Minute

// Match markers in highlighted search results.
const (
	MarkStart = "\x02"
	MarkEnd   = "\x03"
)

const storeSchema = `
CREATE TABLE posts (
	id           TEXT PRIMARY KEY,
	group_id     TEXT NOT NULL,
	timestamp    TEXT NOT NULL,
	subgroup_ids TEXT NOT NULL, -- JSON array
	json         TEXT NOT NULL, -- the post as fetched, with comments
	hash         TEXT NOT NULL
);
CREATE INDEX posts_group ON posts (group_id, timestamp);
CREATE VIRTUAL TABLE posts_fts USING fts5 (
	post_id UNINDEXED, title, body, comments,
	tokenize = "unicode61 remove_diacritics 2"
);
CREATE TABLE synced (
	group_id  TEXT PRIMARY KEY,
	synced_at TEXT NOT NULL
);`

// defaultStoreFile is posts.db in the per-user cache directory:
// %LocalAppData%\spondcli on Windows, ~/Library/Caches/spondcli on macOS
// and $XDG_CACHE_HOME/spondcli (~/.cache/spondcli) elsewhere.
func defaultStoreFile() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "spondcli", "posts.db"), nil
}

func OpenDefault() (*Store, error) {
	path, err := defaultStoreFile()
	if err != nil {
		return nil, err
	}
	return Open(path)
}

// Open opens or creates the cache. WAL and a busy timeout let the TUI
// and a CLI search use it at the same time.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening post cache %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == storeSchemaVersion {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"posts", "posts_fts", "synced"} {
		if _, err := tx.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(storeSchema); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", storeSchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// SyncStats counts what a sync changed in the cache.
type SyncStats struct {
	Posts, Added, Changed, Removed int
}

// sync downloads every post of g and makes the cache match it. A group's
// feed leaves out posts sent to subgroups, so each subgroup's feed is
// fetched too. Nothing is written unless every download succeeds, so a
// failed sync never removes cached posts.
func (s *Store) sync(ctx context.Context, c *spondapi.Client, g spondapi.Group) (SyncStats, error) {
	scopes := []spondapi.Scope{{GroupID: g.ID}}
	for _, sg := range g.SubGroups {
		scopes = append(scopes, spondapi.Scope{GroupID: g.ID, SubgroupID: sg.ID})
	}
	fetched := map[string]spondapi.Post{} // a post sent to several subgroups appears in each feed
	for _, sc := range scopes {
		posts, err := c.AllPosts(ctx, sc)
		if err != nil {
			return SyncStats{}, err
		}
		for _, p := range posts {
			p.GroupID = g.ID
			fetched[p.ID] = p
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SyncStats{}, err
	}
	defer tx.Rollback()

	cached := map[string]string{} // id -> hash
	rows, err := tx.QueryContext(ctx, "SELECT id, hash FROM posts WHERE group_id = ?", g.ID)
	if err != nil {
		return SyncStats{}, err
	}
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			rows.Close()
			return SyncStats{}, err
		}
		cached[id] = hash
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return SyncStats{}, err
	}

	stats := SyncStats{Posts: len(fetched)}
	for id, p := range fetched {
		data, hash, err := postHash(p)
		if err != nil {
			return SyncStats{}, err
		}
		old, ok := cached[id]
		switch {
		case !ok:
			stats.Added++
		case old != hash:
			stats.Changed++
		default:
			continue
		}
		if err := upsertPost(ctx, tx, p, data, hash); err != nil {
			return SyncStats{}, err
		}
	}
	for id := range cached {
		if _, ok := fetched[id]; ok {
			continue
		}
		stats.Removed++
		if err := deletePost(ctx, tx, id); err != nil {
			return SyncStats{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO synced (group_id, synced_at) VALUES (?, ?)
		ON CONFLICT (group_id) DO UPDATE SET synced_at = excluded.synced_at`,
		g.ID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return SyncStats{}, err
	}
	return stats, tx.Commit()
}

// postHash is the stored JSON of p and its hash.
func postHash(p spondapi.Post) (data []byte, hash string, err error) {
	data, err = json.Marshal(p)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// upsertPost stores p and re-indexes its text.
func upsertPost(ctx context.Context, tx *sql.Tx, p spondapi.Post, data []byte, hash string) error {
	ids := p.SubGroupIDs
	if ids == nil {
		ids = []string{} // json_each needs an array, not null
	}
	subgroups, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO posts (id, group_id, timestamp, subgroup_ids, json, hash) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET group_id = excluded.group_id, timestamp = excluded.timestamp,
			subgroup_ids = excluded.subgroup_ids, json = excluded.json, hash = excluded.hash`,
		p.ID, p.GroupID, p.Timestamp, string(subgroups), string(data), hash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM posts_fts WHERE post_id = ?", p.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO posts_fts (post_id, title, body, comments) VALUES (?, ?, ?, ?)",
		p.ID, p.Title, p.Body, commentText(p.Comments))
	return err
}

func deletePost(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM posts WHERE id = ?", id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM posts_fts WHERE post_id = ?", id)
	return err
}

// PutPost stores one post that the activity stream says changed, so search
// sees it before the group's next full sync. p.GroupID must be set.
func (s *Store) PutPost(ctx context.Context, p spondapi.Post) error {
	data, hash, err := postHash(p)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertPost(ctx, tx, p, data, hash); err != nil {
		return err
	}
	return tx.Commit()
}

// RemovePost drops a post that no longer exists.
func (s *Store) RemovePost(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deletePost(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// commentSep separates comments in the index. The tokenizer treats it as
// whitespace, and it never occurs in comment text, so a highlighted
// comments column splits back into one entry per comment.
const commentSep = "\x1e"

// ThreadComment is a comment or reply in display order.
type ThreadComment struct {
	spondapi.Comment
	Reply bool
}

// FlattenComments lists comments oldest first, each followed by its
// replies, oldest first. The API returns both newest first.
func FlattenComments(comments []spondapi.Comment) []ThreadComment {
	var out []ThreadComment
	for _, c := range byTime(comments) {
		out = append(out, ThreadComment{Comment: c})
		for _, r := range byTime(c.Children) {
			out = append(out, ThreadComment{Comment: r, Reply: true})
		}
	}
	return out
}

func byTime(comments []spondapi.Comment) []spondapi.Comment {
	return slices.SortedStableFunc(slices.Values(comments), func(a, b spondapi.Comment) int {
		return cmp.Compare(a.Timestamp, b.Timestamp)
	})
}

// commentText joins the text of comments and replies, in flattenComments
// order, for indexing.
func commentText(comments []spondapi.Comment) string {
	flat := FlattenComments(comments)
	texts := make([]string, len(flat))
	for i, c := range flat {
		texts[i] = strings.ReplaceAll(c.Text, commentSep, " ")
	}
	return strings.Join(texts, commentSep)
}

// syncedAt returns when g was last synced; ok is false if it never was.
func (s *Store) syncedAt(ctx context.Context, groupID string) (t time.Time, ok bool, err error) {
	var at string
	err = s.db.QueryRowContext(ctx, "SELECT synced_at FROM synced WHERE group_id = ?", groupID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err = time.Parse(time.RFC3339Nano, at)
	return t, err == nil, err
}

// Freshen syncs g when force is set or its cache is older than syncMaxAge.
// stats is nil when no sync ran. When the sync fails, usable reports
// whether an older copy is cached, so the caller can warn and search it
// anyway.
func (s *Store) Freshen(ctx context.Context, c *spondapi.Client, g spondapi.Group, force bool) (stats *SyncStats, usable bool, err error) {
	at, ok, err := s.syncedAt(ctx, g.ID)
	if err != nil {
		return nil, false, err
	}
	if ok && !force && time.Since(at) < syncMaxAge {
		return nil, true, nil
	}
	st, err := s.sync(ctx, c, g)
	if err != nil {
		return nil, ok, fmt.Errorf("syncing posts: %w", err)
	}
	return &st, true, nil
}

// SearchHit is a post matching a search. Title, Body and Comments carry
// the post's text with matches wrapped in markStart and markEnd; Comments
// lines up with flattenComments(Post.Comments). Snippet is an excerpt of
// the best matching field.
type SearchHit struct {
	Post     spondapi.Post
	Title    string
	Body     string
	Comments []string
	Snippet  string
}

// PlainHit wraps a post that is shown without a search.
func PlainHit(p spondapi.Post) SearchHit {
	flat := FlattenComments(p.Comments)
	comments := make([]string, len(flat))
	for i, c := range flat {
		comments[i] = c.Text
	}
	return SearchHit{Post: p, Title: p.Title, Body: p.Body, Comments: comments}
}

// CommentMatches lists the indexes of comments containing a match.
func (h SearchHit) CommentMatches() []int {
	var out []int
	for i, c := range h.Comments {
		if strings.Contains(c, MarkStart) {
			out = append(out, i)
		}
	}
	return out
}

// PostMatches reports whether the title or body contains a match.
func (h SearchHit) PostMatches() bool {
	return strings.Contains(h.Title, MarkStart) || strings.Contains(h.Body, MarkStart)
}

// Search finds posts in a group, optionally only those sent to a subgroup,
// that contain every word of query (prefix match, ignoring case and
// accents). Newest posts come first.
func (s *Store) Search(ctx context.Context, groupID, subgroupID, query string) ([]SearchHit, error) {
	match := matchQuery(query)
	if match == "" {
		return nil, errors.New("nothing to search for")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.json,
			highlight(posts_fts, 1, ?1, ?2),
			highlight(posts_fts, 2, ?1, ?2),
			highlight(posts_fts, 3, ?1, ?2),
			snippet(posts_fts, -1, ?1, ?2, '…', 24)
		FROM posts_fts JOIN posts p ON p.id = posts_fts.post_id
		WHERE posts_fts MATCH ?3 AND p.group_id = ?4
			AND (?5 = '' OR EXISTS (SELECT 1 FROM json_each(p.subgroup_ids) WHERE value = ?5))
		ORDER BY p.timestamp DESC`,
		MarkStart, MarkEnd, match, groupID, subgroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []SearchHit
	for rows.Next() {
		var data, comments string
		var h SearchHit
		if err := rows.Scan(&data, &h.Title, &h.Body, &comments, &h.Snippet); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &h.Post); err != nil {
			return nil, err
		}
		if len(h.Post.Comments) > 0 {
			h.Comments = strings.Split(comments, commentSep)
		}
		if len(h.Comments) != len(FlattenComments(h.Post.Comments)) {
			h.Comments = PlainHit(h.Post).Comments // never expected; fall back to unmarked text
		}
		h.Snippet = strings.ReplaceAll(h.Snippet, commentSep, " ")
		// The cache may predate sanitising in the client. Keep the match
		// markers, which are control characters themselves.
		spondapi.Sanitize(&h.Post)
		h.Title, h.Body, h.Snippet = cleanMarked(h.Title), cleanMarked(h.Body), cleanMarked(h.Snippet)
		for i, c := range h.Comments {
			h.Comments[i] = cleanMarked(c)
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// matchQuery turns free text into an FTS5 query: every word must occur,
// as a prefix. Quoting each word keeps FTS5 syntax (AND, *, quotes) in the
// input from being interpreted.
func matchQuery(text string) string {
	words := strings.Fields(text)
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(words, " ")
}

// Unmark replaces the match markers in s with on and off.
func Unmark(s, on, off string) string {
	return strings.NewReplacer(MarkStart, on, MarkEnd, off).Replace(s)
}

// cleanMarked is spondapi.CleanText that keeps MarkStart and MarkEnd.
func cleanMarked(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r == rune(MarkStart[0]) || r == rune(MarkEnd[0]) || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}
