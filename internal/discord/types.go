package discord

import (
	"strconv"
	"time"
)

// Channel types returned by the Discord API that we care about.
const (
	ChannelTypeGuildText  = 0
	ChannelTypeDM         = 1
	ChannelTypeGroupDM    = 3
	ChannelTypeGuildVoice = 2
)

// User is the subset of a Discord user object we use.
type User struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	GlobalName    string `json:"global_name"`
}

// DisplayName returns the friendliest available name for the user.
func (u User) DisplayName() string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	if u.Discriminator != "" && u.Discriminator != "0" {
		return u.Username + "#" + u.Discriminator
	}
	return u.Username
}

// Guild is the subset of a guild (server) object we use.
type Guild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Channel is the subset of a channel object we use.
type Channel struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	Name       string `json:"name"`
	GuildID    string `json:"guild_id"`
	Recipients []User `json:"recipients"`
}

// IsDM reports whether the channel is a direct or group DM.
func (c Channel) IsDM() bool {
	return c.Type == ChannelTypeDM || c.Type == ChannelTypeGroupDM
}

// Label returns a human-friendly description of the channel.
func (c Channel) Label() string {
	switch {
	case c.Name != "":
		return "#" + c.Name
	case c.Type == ChannelTypeDM && len(c.Recipients) > 0:
		return "DM with " + c.Recipients[0].DisplayName()
	case c.Type == ChannelTypeGroupDM:
		return "group DM"
	default:
		return "channel " + c.ID
	}
}

// Message is the subset of a message object we use.
type Message struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Author    User      `json:"author"`
	Pinned    bool      `json:"pinned"`
	Type      int       `json:"type"`

	Attachments []struct {
		ID string `json:"id"`
	} `json:"attachments"`
}

// HasAttachment reports whether the message carries any attachments.
func (m Message) HasAttachment() bool { return len(m.Attachments) > 0 }

// Deletable reports whether a message can actually be deleted. Some system
// message types (e.g. call/join notices) cannot be removed by the user.
func (m Message) Deletable() bool {
	// 0 = default, 19 = reply. Other user-authored types vary, but these two
	// cover the overwhelming majority of deletable content.
	switch m.Type {
	case 0, 19:
		return true
	default:
		return false
	}
}

// searchResponse is the shape of the message search endpoint.
type searchResponse struct {
	TotalResults int         `json:"total_results"`
	Messages     [][]Message `json:"messages"`
}

// snowflakeFromTime converts a time to a Discord snowflake ID, useful for
// building before/after query bounds from timestamps.
func snowflakeFromTime(t time.Time) string {
	const discordEpoch = 1420070400000 // 2015-01-01 in ms
	ms := t.UnixMilli() - discordEpoch
	if ms < 0 {
		ms = 0
	}
	return strconv.FormatInt(ms<<22, 10)
}
