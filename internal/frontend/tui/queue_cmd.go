package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/base/i18n"
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
		return queuePauseCommand(ctx, c, true)
	case "resume":
		return queuePauseCommand(ctx, c, false)
	case "retry":
		return withQueueEntry(ctx, c, rest, "retry", func(it InboxItem) string {
			if mayHaveRun(it) {
				return retryHeldText(it)
			}
			if err := c.InboxRetry(ctx, it.ID); err != nil {
				return "retry: " + err.Error()
			}
			return "retry queued #" + shortID(it.ID)
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

// queueReadingVerbs are the /queue verbs that only look. Every other verb
// mutates the queue, and a newer schema refuses every mutation, so a declared
// list beats inferring it from the verb's spelling.
var queueReadingVerbs = map[string]bool{"show": true}

// queuePauseCommand pauses or resumes the queue, unless a newer schema owns it.
func queuePauseCommand(ctx context.Context, c *Client, paused bool) slashDoneMsg {
	verb := "resume"
	if paused {
		verb = "pause"
	}
	snap, err := c.Inbox(ctx)
	if err != nil {
		return info(verb + ": " + err.Error())
	}
	if snap.Readonly {
		return info(i18n.M.QueueReadonlyNote)
	}
	if err := c.InboxPause(ctx, paused); err != nil {
		return info(verb + ": " + err.Error())
	}
	if paused {
		return info("inbox paused")
	}
	return info("inbox resumed")
}

func withQueueItem(ctx context.Context, c *Client, ref []string, verb string, run func(id string) string) slashDoneMsg {
	return withQueueEntry(ctx, c, ref, verb, func(it InboxItem) string { return run(it.ID) })
}

// withQueueEntry is withQueueItem for a verb that decides from the entry
// itself, such as the retry rule that reads its block code.
func withQueueEntry(ctx context.Context, c *Client, ref []string, verb string, run func(InboxItem) string) slashDoneMsg {
	snap, err := c.Inbox(ctx)
	if err != nil {
		return info(verb + ": " + err.Error())
	}
	if !queueReadingVerbs[verb] && snap.Readonly {
		return info(i18n.M.QueueReadonlyNote)
	}
	id, err := resolveQueueRef(snap, ref)
	if err != nil {
		return info(err.Error())
	}
	for _, it := range snap.Items {
		if it.ID == id {
			return info(run(it))
		}
	}
	return info(verb + ": #" + shortID(id) + " is no longer in the queue")
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
		if snap.Unrecovered {
			return i18n.M.TUIQueueUnrecovered
		}
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
	if snap.Readonly {
		// A newer schema owns the queue: every mutation is refused, so the
		// listing says so beside the state the person would act on.
		b.WriteString(" readonly")
	}
	if snap.Recovered {
		fmt.Fprintf(&b, " recovered=%d", snap.RecoveredN)
	}
	if snap.Unrecovered {
		// The items a corrupt manifest still names say nothing about what the
		// unreadable bodies hold, so the unknown marker outranks the count.
		fmt.Fprintf(&b, "\n%s", i18n.M.TUIQueueUnrecovered)
	}
	b.WriteByte('\n')
	for i, it := range snap.Items[:min(len(snap.Items), queueListLimit)] {
		fmt.Fprintf(&b, "  %d. [%s/%s] %s #%s", i+1, it.Intent, it.State, it.Preview, shortID(it.ID))
		if it.Origin == "host" {
			fmt.Fprintf(&b, " (%s)", i18n.M.QueueHostTag)
		}
		if it.Images && it.State == "queued" {
			fmt.Fprintf(&b, " (%s)", i18n.M.QueueImageWait)
		}
		b.WriteByte('\n')
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
