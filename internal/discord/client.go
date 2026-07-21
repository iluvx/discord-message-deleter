// Package discord is a small HTTP client for the Discord API, scoped to what
// this tool needs: identifying the current user, enumerating guilds and DM
// channels, finding the user's own messages, and deleting them. It talks to
// the API directly (rather than via a bot library) because message deletion
// here is performed with a user token.
package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	baseURL   = "https://discord.com/api/v10"
	userAgent = "discord-message-deleter (https://github.com/iluvx/discord-message-deleter, 1.0)"
)

// Client is a rate-limit-aware Discord API client.
type Client struct {
	token string
	http  *http.Client

	// minDelete is the minimum delay between delete requests, added on top of
	// any rate-limit headers Discord returns, to stay comfortably within the
	// per-route limits.
	minDelete time.Duration
}

// New builds a client for the given user token.
func New(token string) *Client {
	return &Client{
		token:     token,
		http:      &http.Client{Timeout: 30 * time.Second},
		minDelete: 750 * time.Millisecond,
	}
}

// APIError represents a non-2xx response from Discord.
type APIError struct {
	Status  int
	Message string
	Code    int
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("discord API error %d (code %d): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("discord API error %d: %s", e.Status, e.Message)
}

// do performs a request, transparently retrying on 429 rate limits. It returns
// the response body for 2xx responses and an *APIError otherwise.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		var reqBody io.Reader
		if body != nil {
			// body may need to be re-read on retry; callers pass *bytes.Reader
			// where relevant, but for our usage body is always nil.
			reqBody = body
		}

		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reqBody)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", c.token)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := parseRetryAfter(resp, data)
			if attempt > 10 {
				return nil, &APIError{Status: resp.StatusCode, Message: "rate limited too many times"}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue

		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return data, nil

		default:
			apiErr := &APIError{Status: resp.StatusCode, Message: string(data)}
			var parsed struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			}
			if json.Unmarshal(data, &parsed) == nil && parsed.Message != "" {
				apiErr.Message = parsed.Message
				apiErr.Code = parsed.Code
			}
			return nil, apiErr
		}
	}
}

// parseRetryAfter determines how long to wait after a 429, preferring the JSON
// body's retry_after (seconds) and falling back to the header.
func parseRetryAfter(resp *http.Response, body []byte) time.Duration {
	var parsed struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.RetryAfter > 0 {
		return time.Duration(parsed.RetryAfter*float64(time.Second)) + 100*time.Millisecond
	}
	if h := resp.Header.Get("Retry-After"); h != "" {
		if secs, err := strconv.ParseFloat(h, 64); err == nil {
			return time.Duration(secs*float64(time.Second)) + 100*time.Millisecond
		}
	}
	return time.Second
}

// CurrentUser returns the account the token belongs to. It doubles as a token
// validity check.
func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	data, err := c.do(ctx, http.MethodGet, "/users/@me", nil)
	if err != nil {
		return nil, err
	}
	var u User
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Guilds returns the guilds the user is a member of.
func (c *Client) Guilds(ctx context.Context) ([]Guild, error) {
	data, err := c.do(ctx, http.MethodGet, "/users/@me/guilds", nil)
	if err != nil {
		return nil, err
	}
	var guilds []Guild
	if err := json.Unmarshal(data, &guilds); err != nil {
		return nil, err
	}
	return guilds, nil
}

// DMChannels returns the user's open DM and group DM channels.
func (c *Client) DMChannels(ctx context.Context) ([]Channel, error) {
	data, err := c.do(ctx, http.MethodGet, "/users/@me/channels", nil)
	if err != nil {
		return nil, err
	}
	var channels []Channel
	if err := json.Unmarshal(data, &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

// Channel fetches a single channel by ID, used to classify a channel the user
// passed explicitly.
func (c *Client) Channel(ctx context.Context, id string) (*Channel, error) {
	data, err := c.do(ctx, http.MethodGet, "/channels/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var ch Channel
	if err := json.Unmarshal(data, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// Guild fetches a single guild by ID.
func (c *Client) Guild(ctx context.Context, id string) (*Guild, error) {
	data, err := c.do(ctx, http.MethodGet, "/guilds/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var g Guild
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// SearchGuildMessages returns all of authorID's messages in the guild,
// optionally restricted to a single channel. It pages through the search
// endpoint until every result is collected.
func (c *Client) SearchGuildMessages(ctx context.Context, guildID, authorID, channelID string) ([]Message, error) {
	var out []Message
	offset := 0

	for {
		q := url.Values{}
		q.Set("author_id", authorID)
		if channelID != "" {
			q.Set("channel_id", channelID)
		}
		q.Set("offset", strconv.Itoa(offset))

		path := "/guilds/" + url.PathEscape(guildID) + "/messages/search?" + q.Encode()
		data, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}

		var sr searchResponse
		if err := json.Unmarshal(data, &sr); err != nil {
			return nil, err
		}

		got := 0
		for _, group := range sr.Messages {
			for _, m := range group {
				if m.Author.ID == authorID {
					out = append(out, m)
					got++
				}
			}
		}

		offset += 25 // search pages are 25 results wide
		if offset >= sr.TotalResults || got == 0 {
			break
		}
	}
	return out, nil
}

// ChannelMessagesByAuthor pages through a channel's messages (newest first)
// and returns those written by authorID. Used for DMs, which the guild search
// endpoint does not cover.
func (c *Client) ChannelMessagesByAuthor(ctx context.Context, channelID, authorID string) ([]Message, error) {
	var out []Message
	before := ""

	for {
		q := url.Values{}
		q.Set("limit", "100")
		if before != "" {
			q.Set("before", before)
		}

		path := "/channels/" + url.PathEscape(channelID) + "/messages?" + q.Encode()
		data, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}

		var batch []Message
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}

		for _, m := range batch {
			if m.Author.ID == authorID {
				out = append(out, m)
			}
		}

		before = batch[len(batch)-1].ID
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

// DeleteMessage removes a single message, pausing afterwards to respect the
// delete route's rate limit.
func (c *Client) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	path := "/channels/" + url.PathEscape(channelID) + "/messages/" + url.PathEscape(messageID)
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.minDelete):
	}
	return nil
}
