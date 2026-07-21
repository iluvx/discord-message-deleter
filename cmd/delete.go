package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/iluvx/discord-message-deleter/internal/config"
	"github.com/iluvx/discord-message-deleter/internal/discord"
	"github.com/iluvx/discord-message-deleter/internal/ui"
	"github.com/spf13/cobra"
)

// deleteFlags holds the parsed command-line options for the delete command.
type deleteFlags struct {
	token    string
	all      bool
	servers  []string
	channels []string
	contains string
	before   string
	after    string
	ignore   []string
	yes      bool
}

var delFlags deleteFlags

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete your messages from servers, channels, or DMs",
	Long: `Delete messages you have sent. Choose what to target with --all, --server,
and --channel, and narrow it down with the filter flags.

Targets:
  --all              every server you are in, plus every open DM
  --server <id>      a specific server (repeatable)
  --channel <id>     a specific channel or DM (repeatable)

Filters (combine freely; all must match):
  --contains <text>  only messages whose content contains this text
  --before <date>    only messages sent before a date (YYYY-MM-DD or RFC3339)
  --after <date>     only messages sent after a date

Exclusions:
  --ignore <id>      skip this ID — a server, channel, or message (repeatable).
                     Handy with --all, e.g. delete everything except one server.

Safety:
  --yes              skip the confirmation prompt

Examples:
  discord-message-deleter delete --all
  discord-message-deleter delete --all --ignore 123456789 --ignore 987654321
  discord-message-deleter delete --server 123456789 --contains "typo"
  discord-message-deleter delete --channel 987654321 --before 2024-01-01 --yes`,
	RunE: runDelete,
}

func init() {
	f := deleteCmd.Flags()
	f.StringVarP(&delFlags.token, "token", "t", "", "Discord token (overrides config and $DISCORD_TOKEN)")
	f.BoolVar(&delFlags.all, "all", false, "Target every server and DM you have")
	f.StringSliceVarP(&delFlags.servers, "server", "s", nil, "Server (guild) ID to target (repeatable)")
	f.StringSliceVarP(&delFlags.channels, "channel", "c", nil, "Channel or DM ID to target (repeatable)")
	f.StringVar(&delFlags.contains, "contains", "", "Only delete messages containing this text")
	f.StringVar(&delFlags.before, "before", "", "Only delete messages before this date (YYYY-MM-DD or RFC3339)")
	f.StringVar(&delFlags.after, "after", "", "Only delete messages after this date (YYYY-MM-DD or RFC3339)")
	f.StringSliceVarP(&delFlags.ignore, "ignore", "i", nil, "ID to skip — server, channel, or message (repeatable)")
	f.BoolVarP(&delFlags.yes, "yes", "y", false, "Skip the confirmation prompt")

	rootCmd.AddCommand(deleteCmd)
}

// filter captures the parsed, ready-to-apply message filters.
type filter struct {
	contains  string
	before    time.Time
	after     time.Time
	hasBefore bool
	hasAfter  bool

	// ignore is a set of IDs (servers, channels, or messages) to skip. Guilds
	// and channels are also skipped earlier during scanning; this catches the
	// message- and channel-level cases when a target is scanned as a whole
	// (e.g. under --all).
	ignore map[string]struct{}
}

func (fl filter) match(m discord.Message) bool {
	if !m.Deletable() {
		return false
	}
	if fl.isIgnored(m.ID) || fl.isIgnored(m.ChannelID) {
		return false
	}
	if fl.contains != "" && !strings.Contains(strings.ToLower(m.Content), strings.ToLower(fl.contains)) {
		return false
	}
	if fl.hasBefore && !m.Timestamp.Before(fl.before) {
		return false
	}
	if fl.hasAfter && !m.Timestamp.After(fl.after) {
		return false
	}
	return true
}

// isIgnored reports whether an ID is in the ignore set.
func (fl filter) isIgnored(id string) bool {
	if id == "" || fl.ignore == nil {
		return false
	}
	_, ok := fl.ignore[id]
	return ok
}

func runDelete(cmd *cobra.Command, args []string) error {
	ui.Banner()

	// Resolve token: flag > env > config.
	token, err := resolveToken(delFlags.token)
	if err != nil {
		ui.Error("%s", err)
		return errSilent
	}

	// Validate targets.
	if !delFlags.all && len(delFlags.servers) == 0 && len(delFlags.channels) == 0 {
		ui.Error("Nothing to target. Pass --all, --server <id>, or --channel <id>.")
		return errSilent
	}

	// Parse filters.
	fl, err := buildFilter()
	if err != nil {
		ui.Error("%s", err)
		return errSilent
	}

	// Cancel cleanly on Ctrl-C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := discord.New(token)

	me, err := client.CurrentUser(ctx)
	if err != nil {
		ui.Error("Could not authenticate with Discord: %v", err)
		ui.Warn("Check that your token is valid (config set-token).")
		return errSilent
	}
	ui.Field("account", "%s %s", me.DisplayName(), ui.Dim("("+me.ID+")"))
	printActiveFilters(fl)
	fmt.Println()

	// Collect messages from every requested target.
	messages, err := collectMessages(ctx, client, me.ID, fl.ignore)
	if err != nil {
		ui.Error("%v", err)
		return errSilent
	}

	// Apply filters and de-duplicate.
	matches := applyFilters(messages, fl)
	if len(matches) == 0 {
		ui.Warn("No messages matched. Nothing to do.")
		return nil
	}

	// Sort newest first for a readable summary.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Timestamp.After(matches[j].Timestamp)
	})

	ui.Rule()
	ui.Info("Found %s message(s) to delete.", ui.Count(len(matches)))
	ui.Rule()

	// Final confirmation before destructive action.
	if !delFlags.yes {
		fmt.Println()
		if !ui.Confirm(fmt.Sprintf("Permanently delete these %d message(s)?", len(matches))) {
			ui.Warn("Aborted. Nothing was deleted.")
			return nil
		}
		fmt.Println()
	}

	return performDeletes(ctx, client, matches)
}

// resolveToken picks the token from the flag, environment, or config, in that
// order of precedence.
func resolveToken(flagToken string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if env := strings.TrimSpace(os.Getenv("DISCORD_TOKEN")); env != "" {
		return env, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if cfg.Token != "" {
		return cfg.Token, nil
	}
	return "", fmt.Errorf("no token found — run 'discord-message-deleter config set-token', set $DISCORD_TOKEN, or pass --token")
}

// buildFilter parses the filter flags into a filter value.
func buildFilter() (filter, error) {
	fl := filter{contains: delFlags.contains}

	if ids := dedupe(delFlags.ignore); len(ids) > 0 {
		fl.ignore = make(map[string]struct{}, len(ids))
		for _, id := range ids {
			fl.ignore[id] = struct{}{}
		}
	}

	if delFlags.before != "" {
		t, err := parseDate(delFlags.before)
		if err != nil {
			return fl, fmt.Errorf("invalid --before value: %w", err)
		}
		fl.before = t
		fl.hasBefore = true
	}
	if delFlags.after != "" {
		t, err := parseDate(delFlags.after)
		if err != nil {
			return fl, fmt.Errorf("invalid --after value: %w", err)
		}
		fl.after = t
		fl.hasAfter = true
	}
	if fl.hasBefore && fl.hasAfter && !fl.after.Before(fl.before) {
		return fl, fmt.Errorf("--after (%s) must be earlier than --before (%s)",
			delFlags.after, delFlags.before)
	}
	return fl, nil
}

// parseDate accepts a plain date or a full RFC3339 timestamp.
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("use YYYY-MM-DD or an RFC3339 timestamp, got %q", s)
}

// collectMessages gathers the user's messages from every requested target,
// keyed to avoid duplicates when targets overlap.
func collectMessages(ctx context.Context, client *discord.Client, selfID string, ignore map[string]struct{}) ([]discord.Message, error) {
	seen := make(map[string]discord.Message)

	ignored := func(id string) bool {
		if ignore == nil {
			return false
		}
		_, ok := ignore[id]
		return ok
	}

	// add stores messages, skipping any whose channel or ID is ignored. This
	// covers channels excluded within a guild that was scanned as a whole.
	add := func(msgs []discord.Message) {
		for _, m := range msgs {
			if ignored(m.ID) || ignored(m.ChannelID) {
				continue
			}
			seen[m.ID] = m
		}
	}

	// --all expands to every guild plus every open DM.
	servers := delFlags.servers
	channels := delFlags.channels

	if delFlags.all {
		guilds, err := client.Guilds(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not list your servers: %w", err)
		}
		for _, g := range guilds {
			servers = append(servers, g.ID)
		}

		dms, err := client.DMChannels(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not list your DMs: %w", err)
		}
		for _, d := range dms {
			channels = append(channels, d.ID)
		}
	}

	// Servers: search the whole guild for the user's messages.
	for _, gid := range dedupe(servers) {
		if ignored(gid) {
			ui.Info("  ignoring server %s", ui.Dim(gid))
			continue
		}
		name := gid
		if g, err := client.Guild(ctx, gid); err == nil {
			name = g.Name
		}
		ui.Step("Scanning server %s ...", ui.Accent(name))

		msgs, err := client.SearchGuildMessages(ctx, gid, selfID, "")
		if err != nil {
			ui.Warn("  skipped server %s: %v", gid, err)
			continue
		}
		ui.Info("  found %s of your message(s)", ui.Count(len(msgs)))
		add(msgs)
	}

	// Channels: classify each, then use guild search or DM pagination.
	for _, cid := range dedupe(channels) {
		if ignored(cid) {
			ui.Info("  ignoring channel %s", ui.Dim(cid))
			continue
		}
		ch, err := client.Channel(ctx, cid)
		if err != nil {
			ui.Warn("  skipped channel %s: %v", cid, err)
			continue
		}
		ui.Step("Scanning %s ...", ui.Accent(ch.Label()))

		var msgs []discord.Message
		if ch.IsDM() || ch.GuildID == "" {
			msgs, err = client.ChannelMessagesByAuthor(ctx, cid, selfID)
		} else {
			msgs, err = client.SearchGuildMessages(ctx, ch.GuildID, selfID, cid)
		}
		if err != nil {
			ui.Warn("  skipped %s: %v", ch.Label(), err)
			continue
		}
		ui.Info("  found %s of your message(s)", ui.Count(len(msgs)))
		add(msgs)
	}

	out := make([]discord.Message, 0, len(seen))
	for _, m := range seen {
		out = append(out, m)
	}
	return out, nil
}

// applyFilters returns only the messages matching every active filter.
func applyFilters(messages []discord.Message, fl filter) []discord.Message {
	out := make([]discord.Message, 0, len(messages))
	for _, m := range messages {
		if fl.match(m) {
			out = append(out, m)
		}
	}
	return out
}

// performDeletes deletes each message, reporting progress and a final summary.
func performDeletes(ctx context.Context, client *discord.Client, matches []discord.Message) error {
	var deleted, failed int
	for i, m := range matches {
		if ctx.Err() != nil {
			ui.Warn("Interrupted — stopping.")
			break
		}
		if err := client.DeleteMessage(ctx, m.ChannelID, m.ID); err != nil {
			failed++
			ui.Warn("failed to delete %s: %v", m.ID, err)
			continue
		}
		deleted++
		ui.Deleted("[%d/%d] %s", i+1, len(matches), preview(m))
	}

	fmt.Println()
	ui.Rule()
	ui.Success("Deleted %s message(s).", ui.Count(deleted))
	if failed > 0 {
		ui.Warn("%s message(s) could not be deleted.", ui.Count(failed))
	}
	return nil
}

// printActiveFilters echoes the filters in effect.
func printActiveFilters(fl filter) {
	if fl.contains != "" {
		ui.Field("contains", "%q", fl.contains)
	}
	if fl.hasAfter {
		ui.Field("after", "%s", fl.after.Format("2006-01-02 15:04 MST"))
	}
	if fl.hasBefore {
		ui.Field("before", "%s", fl.before.Format("2006-01-02 15:04 MST"))
	}
	if len(fl.ignore) > 0 {
		ui.Field("ignoring", "%s ID(s)", ui.Count(len(fl.ignore)))
	}
}

// preview renders a compact one-line summary of a message.
func preview(m discord.Message) string {
	content := strings.ReplaceAll(m.Content, "\n", " ")
	content = strings.TrimSpace(content)
	if content == "" {
		if m.HasAttachment() {
			content = "(attachment)"
		} else {
			content = "(no text)"
		}
	}
	const max = 60
	if len(content) > max {
		content = content[:max-1] + "…"
	}
	stamp := m.Timestamp.Format("2006-01-02")
	return fmt.Sprintf("%s  %s", ui.Dim(stamp), content)
}

// dedupe removes duplicate strings while preserving order.
func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
