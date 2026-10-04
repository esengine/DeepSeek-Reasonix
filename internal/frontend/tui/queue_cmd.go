package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const queueUsage = "usage: /queue list|show|edit|delete|move|pause|resume|retry|refresh"

const queueListLimit = 20

// steerCommand sends guidance into the running turn through the kernel's
// inbox, which settles whether it lands now or waits as a follow-up.
func steerCommand(ctx context.Context, c *Client, text string) slashDoneMsg {
	if text == "" {
		return usage("usage: /steer <guidance>")
	}
	rec, err := c.Enqueue(ctx, text, true)
	if err != nil {
		return info("steer: " + err.Error())
	}
	switch rec.Disposition {
	case dispositionSteerAccepted:
		return info(fmt.Sprintf("steer accepted #%s", shortID(rec.ItemID)))
	case dispositionQueuedFollowup:
		return info(fmt.Sprintf("steer rejected — queued as follow-up #%s", shortID(rec.ItemID)))
	}
	return info(fmt.Sprintf("queued #%s (%s)", shortID(rec.ItemID), rec.Disposition))
}

// queueCommand runs one /queue subcommand and returns the line or block to show.
func queueCommand(ctx context.Context, c *Client, args []string) slashDoneMsg {
	if len(args) == 0 {
		args = []string{"list"}
	}
	rest := args[1:]
	switch strings.ToLower(args[0]) {
	case "list", "ls", "status":
		snap, err := c.Inbox(ctx)
		if err != nil {
			return info("list: " + err.Error())
		}
		return info(renderQueueList(snap))
	case "show":
		return withQueueItem(ctx, c, rest, "show", func(id string) string {
			body, err := c.InboxBody(ctx, id)
			if err != nil {
				return "show: " + err.Error()
			}
			return body
		})
	case "edit":
		if len(rest) < 2 {
			return usage("usage: /queue edit <n|id> <text>")
		}
		return withQueueItem(ctx, c, rest[:1], "edit", func(id string) string {
			if err := c.InboxUpdate(ctx, id, strings.Join(rest[1:], " ")); err != nil {
				return "edit: " + err.Error()
			}
			return "updated #" + shortID(id)
		})
	case "delete", "rm", "del":
		return withQueueItem(ctx, c, rest, "delete", func(id string) string {
			if err := c.InboxDelete(ctx, id); err != nil {
				return "delete: " + err.Error()
			}
			return "deleted #" + shortID(id)
		})
	case "move":
		if len(rest) < 2 {
			return usage("usage: /queue move <n|id> <to-index>")
		}
		return withQueueItem(ctx, c, rest[:1], "move", func(id string) string {
			to, err := strconv.Atoi(rest[1])
			if err != nil {
				return "move: bad index"
			}
			if err := c.InboxMove(ctx, id, to-1); err != nil {
				return "move: " + err.Error()
			}
			return "moved #" + shortID(id)
		})
	case "pause":
		if err := c.InboxPause(ctx, true); err != nil {
			return info("pause: " + err.Error())
		}
		return info("inbox paused")
	case "resume":
		if err := c.InboxPause(ctx, false); err != nil {
			return info("resume: " + err.Error())
		}
		return info("inbox resumed")
	case "retry":
		return withQueueItem(ctx, c, rest, "retry", func(id string) string {
			if err := c.InboxRetry(ctx, id); err != nil {
				return "retry: " + err.Error()
			}
			return "retry queued #" + shortID(id)
		})
	case "refresh":
		return withQueueItem(ctx, c, rest, "refresh", func(id string) string {
			if err := c.InboxRefresh(ctx, id); err != nil {
				return "refresh: " + err.Error()
			}
			return "refs refreshed #" + shortID(id)
		})
	}
	return usage(queueUsage)
}

var errMissingQueueRef = errors.New("missing item ref (index or id)")

func withQueueItem(ctx context.Context, c *Client, ref []string, verb string, run func(id string) string) slashDoneMsg {
	snap, err := c.Inbox(ctx)
	if err != nil {
		return info(verb + ": " + err.Error())
	}
	id, err := resolveQueueRef(snap, ref)
	if err != nil {
		return info(err.Error())
	}
	return info(run(id))
}

// resolveQueueRef takes a 1-based position, a full id or an id prefix.
func resolveQueueRef(snap InboxSnapshot, args []string) (string, error) {
	if len(args) == 0 {
		return "", errMissingQueueRef
	}
	ref := args[0]
	if n, err := strconv.Atoi(ref); err == nil {
		if n < 1 || n > len(snap.Items) {
			return "", fmt.Errorf("unknown inbox item %q", ref)
		}
		return snap.Items[n-1].ID, nil
	}
	var match string
	for _, it := range snap.Items {
		if !strings.HasPrefix(it.ID, ref) {
			continue
		}
		if match != "" {
			return "", fmt.Errorf("ambiguous inbox item %q: matches more than one", ref)
		}
		match = it.ID
	}
	if match == "" {
		return "", fmt.Errorf("unknown inbox item %q", ref)
	}
	return match, nil
}

func renderQueueList(snap InboxSnapshot) string {
	if len(snap.Items) == 0 {
		if snap.Paused {
			return "inbox empty (paused)"
		}
		return "inbox empty"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "inbox rev=%d items=%d", snap.Revision, len(snap.Items))
	if snap.Paused {
		b.WriteString(" paused")
	}
	if snap.Recovered {
		fmt.Fprintf(&b, " recovered=%d", snap.RecoveredN)
	}
	b.WriteByte('\n')
	for i, it := range snap.Items[:min(len(snap.Items), queueListLimit)] {
		fmt.Fprintf(&b, "  %d. [%s/%s] %s #%s\n", i+1, it.Intent, it.State, it.Preview, shortID(it.ID))
	}
	if more := len(snap.Items) - queueListLimit; more > 0 {
		fmt.Fprintf(&b, "  … and %d more (use /queue show <n>)\n", more)
	}
	return strings.TrimRight(b.String(), "\n")
}

func info(text string) slashDoneMsg  { return slashDoneMsg{level: "info", text: text} }
func usage(text string) slashDoneMsg { return slashDoneMsg{level: "warn", text: text} }

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
