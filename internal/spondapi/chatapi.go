package spondapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Messages (ChatApi) live on a separate chat server. POST core/v1/chat/
// returns its base URL and a token, which goes in an "auth" header (not
// Authorization). See docs/API.md.

type ChatAuth struct {
	URL  string `json:"url"`
	Auth string `json:"auth"`
}

// ChatThread is a conversation (ApiChatThread). Type is GROUP, DM,
// CLUB_MESSAGE or CAMPAIGN; Name is often empty. Participants are profile ids.
type ChatThread struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	GroupID         string       `json:"groupId"`
	Participants    []string     `json:"participants"`
	NewestTimestamp string       `json:"newestTimestamp"`
	Unread          bool         `json:"unread"`
	Muted           bool         `json:"muted"`
	Message         *ChatMessage `json:"message"` // newest message
}

// ChatMessage is one message (ApiChatMessage). MsgNum counts up from 0
// within a thread. User is the sender's profile id; club messages and
// campaigns have none.
type ChatMessage struct {
	ChatID    string    `json:"chatId"`
	MsgNum    int       `json:"msgNum"`
	User      string    `json:"user"`
	Text      string    `json:"text"`
	Type      string    `json:"type"` // TEXT, IMAGES, CLUB_MESSAGE, CAMPAIGN, …
	Timestamp string    `json:"timestamp"`
	Deleted   bool      `json:"deleted"`
	Mentions  []Mention `json:"mentions"` // in the order of the marks in Text
	// Reactions maps an emoji to who reacted with it (profile id → epoch
	// ms). Everyone has at most one reaction per message.
	Reactions   map[string]map[string]int64 `json:"reactions"`
	ClubMessage *clubMessage                `json:"clubMessage"`
	Campaign    *struct {
		CampaignName string `json:"campaignName"`
	} `json:"campaign"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	File *struct {
		Name string `json:"name"`
	} `json:"file"`
}

type clubMessage struct {
	ClubName    string `json:"clubName"`
	SenderName  string `json:"senderName"`
	SenderTitle string `json:"senderTitle"`
	Subject     string `json:"subject"`
}

// chatToken returns the cached chat server login, fetching a new one when
// there is none or refresh is set.
func (c *Client) chatToken(ctx context.Context, refresh bool) (ChatAuth, error) {
	c.chatMu.Lock()
	defer c.chatMu.Unlock()
	if c.Chat.Auth != "" && !refresh {
		return c.Chat, nil
	}
	var a ChatAuth
	if err := c.do(ctx, http.MethodPost, "chat/", nil, struct{}{}, &a); err != nil {
		return ChatAuth{}, err
	}
	if a.URL == "" || a.Auth == "" {
		return ChatAuth{}, errors.New("chat login: response has no url or token")
	}
	c.Chat = a
	return a, nil
}

// chatDo makes a request to the chat server, logging in to it again once
// if the token was rejected.
func (c *Client) chatDo(ctx context.Context, method, path string, query url.Values, body, out any) error {
	for attempt := range 2 {
		a, err := c.chatToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		header := http.Header{"Auth": {a.Auth}}
		err = c.send(ctx, method, strings.TrimSuffix(a.URL, "/")+"/"+path, query, header, body, out)
		if !IsUnauthorized(err) || attempt > 0 {
			return err
		}
	}
	panic("unreachable")
}

func (c *Client) chatGet(ctx context.Context, path string, query url.Values, out any) error {
	return c.chatDo(ctx, http.MethodGet, path, query, nil, out)
}

// Chats lists conversations, newest first. before is the newestTimestamp of
// the last thread already shown, or "" for the first page.
func (c *Client) Chats(ctx context.Context, max int, before string) ([]ChatThread, error) {
	query := url.Values{"max": {strconv.Itoa(max)}}
	if before != "" {
		query.Set("maxTimestamp", before)
	}
	threads := []ChatThread{}
	if err := c.chatGet(ctx, "chats", query, &threads); err != nil {
		return nil, err
	}
	return threads, nil
}

// ChatMessages returns up to count messages of a thread, newest first,
// with msgNum at most maxNum (all when maxNum < 0).
func (c *Client) ChatMessages(ctx context.Context, chatID string, count, maxNum int) ([]ChatMessage, error) {
	query := url.Values{"count": {strconv.Itoa(count)}}
	if maxNum >= 0 {
		query.Set("maxNum", strconv.Itoa(maxNum))
	}
	msgs := []ChatMessage{}
	if err := c.chatGet(ctx, "chats/"+url.PathEscape(chatID)+"/messages", query, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

// FindChat returns the conversation in a group between you and recipients
// (profile ids, not including you), or nil if there is none yet
// (ChatApi.findThread). The server answers {} when there is none.
func (c *Client) FindChat(ctx context.Context, groupID string, recipients []string) (*ChatThread, error) {
	var t ChatThread
	path := "chats/community/" + url.PathEscape(groupID) + "/type/GROUP/find"
	if err := c.chatDo(ctx, http.MethodPost, path, nil, recipients, &t); err != nil {
		return nil, err
	}
	if t.ID == "" {
		return nil, nil
	}
	return &t, nil
}

// SendChat posts a text message to an existing conversation. text holds a
// mentionMark for each of mentions.
func (c *Client) SendChat(ctx context.Context, chatID, text string, mentions []Mention) (*ChatMessage, error) {
	body := map[string]any{"chatId": chatID, "text": text, "type": "TEXT"}
	addMentions(body, mentions)
	var msg ChatMessage
	if err := c.chatDo(ctx, http.MethodPost, "messages", nil, body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// SetReaction makes emoji your reaction to a message, replacing any other
// you had (ChatApi.setReaction). It returns when the server stored it, in
// epoch ms.
func (c *Client) SetReaction(ctx context.Context, chatID string, msgNum int, emoji string) (int64, error) {
	var out struct {
		Value int64 `json:"value"`
	}
	path := "chats/" + url.PathEscape(chatID) + "/" + strconv.Itoa(msgNum) + "/reaction"
	if err := c.chatDo(ctx, http.MethodPut, path, nil, map[string]string{"value": emoji}, &out); err != nil {
		return 0, err
	}
	return out.Value, nil
}

// RemoveReaction removes your reaction to a message.
func (c *Client) RemoveReaction(ctx context.Context, chatID string, msgNum int) error {
	path := "chats/" + url.PathEscape(chatID) + "/" + strconv.Itoa(msgNum) + "/reaction"
	return c.chatDo(ctx, http.MethodDelete, path, nil, nil, nil)
}

// addMentions adds the tagged people to a message body, with only id and
// type as the app sends them.
func addMentions(body map[string]any, mentions []Mention) {
	if len(mentions) == 0 {
		return
	}
	list := make([]map[string]string, len(mentions))
	for i, p := range mentions {
		list[i] = map[string]string{"id": p.ID, "type": p.Type}
	}
	body["mentions"] = list
}

// StartChat posts the first message to people in a group, which creates the
// conversation; the reply's chatId names it. As in the app, one recipient
// goes in "recipient" and several in "recipients".
func (c *Client) StartChat(ctx context.Context, groupID string, recipients []string, text string, mentions []Mention) (*ChatMessage, error) {
	body := map[string]any{
		"community": map[string]string{"type": "GROUP", "id": groupID},
		"text":      text,
		"type":      "TEXT",
	}
	addMentions(body, mentions)
	if len(recipients) == 1 {
		body["recipient"] = recipients[0]
	} else {
		body["recipients"] = recipients
	}
	var msg ChatMessage
	if err := c.chatDo(ctx, http.MethodPost, "messages", nil, body, &msg); err != nil {
		return nil, err
	}
	if msg.ChatID == "" {
		return nil, errors.New("send: the reply names no conversation")
	}
	return &msg, nil
}

// Activity is one entry of the notifications screen (ApiActivitySummary).
// Text is HTML with <b> around names and titles, in the account's language.
// Which ids are set depends on Type (spond_comment, post_comment,
// decline_spond, accept_spond, update_spond, nag, comment_mention, …).
type Activity struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	ProfileID    string `json:"profileId"`
	GroupID      string `json:"groupId"`
	SpondID      string `json:"spondId"`
	PostID       string `json:"postId"`
	MembershipID string `json:"membershipId"`
	CommentID    string `json:"pCmntId"`
	Text         string `json:"text"`
	Timestamp    string `json:"timestamp"`
	Read         bool   `json:"read"`
}

// Activities fetches the notifications list (ActivityApi.getActivitySummaries).
func (c *Client) Activities(ctx context.Context) ([]Activity, error) {
	items := []Activity{}
	if err := c.do(ctx, http.MethodGet, "activities/summary", nil, nil, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// Post fetches one post with its comments.
func (c *Client) Post(ctx context.Context, id string) (*Post, error) {
	var p Post
	query := url.Values{"includeComments": {"true"}}
	if err := c.do(ctx, http.MethodGet, "posts/"+url.PathEscape(id), query, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
