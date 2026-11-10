package control

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// peerCommand serves /peer for the frontends that route raw input through
// Submit: list, send, approve, deny. The name to send to is one word, or quoted
// when a title has spaces.
func (c *Controller) peerCommand(args string) {
	verb, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	rest = strings.TrimSpace(rest)
	if strings.EqualFold(verb, "settings") || !c.PeersEnabled() {
		c.notice(c.peerSettingsText())
		return
	}
	switch strings.ToLower(verb) {
	case "", "list":
		c.notice(c.peerListText())
	case "send":
		name, text, ok := splitPeerName(rest)
		if !ok {
			c.notice(`usage: /peer send <session> <text>   (quote the session if its title has spaces: "my title")`)
			return
		}
		rec, err := c.SendPeerMessage(name, text)
		if err != nil {
			c.notice("peer send: " + peerErrorText(err))
			return
		}
		c.notice(fmt.Sprintf("sent %s to %s; it reaches that session's model as soon as it is free, unless the dispatch switch is off, when it waits for that session's user", rec.ID, peerLabel(rec.To)))
	case "approve", "deny":
		if rest == "" {
			c.notice("usage: /peer " + verb + " <message id>")
			return
		}
		if err := c.ResolvePeerMessage(rest, strings.EqualFold(verb, "approve")); err != nil {
			c.notice("peer " + verb + ": " + peerErrorText(err))
			return
		}
		c.notice("peer " + strings.ToLower(verb) + ": " + rest + " done")
	default:
		c.notice("usage: /peer [list | settings | send <session> <text> | approve <id> | deny <id>]")
	}
}

func (c *Controller) peerListText() string {
	var b strings.Builder
	peers := c.Peers()
	if len(peers) == 0 {
		b.WriteString("no other session is listening on this machine")
	} else {
		b.WriteString("sessions you can message:")
		for _, p := range peers {
			b.WriteString("\n  " + peerLabel(p) + "  (" + p.Key + ")")
		}
	}
	for _, th := range c.PeerThreads() {
		fmt.Fprintf(&b, "\nyour message to %s: %s", peerLabel(PeerView{Key: th.Key, Title: th.Title, Project: th.Project}), th.State)
	}
	for _, h := range c.HeldPeerMessages() {
		fmt.Fprintf(&b, "\nwaiting for your decision  %s  from %s: %s", h.ID, peerLabel(PeerView{Title: h.FromTitle, Project: h.FromProject, Key: h.FromSession}), h.Body)
	}
	return b.String()
}

func peerLabel(p PeerView) string {
	switch {
	case p.Project != "" && p.Title != "":
		return p.Project + "/" + p.Title
	case p.Title != "":
		return p.Title
	}
	return p.Key
}

func splitPeerName(rest string) (name, text string, ok bool) {
	if strings.HasPrefix(rest, `"`) {
		end := strings.Index(rest[1:], `"`)
		if end < 0 {
			return "", "", false
		}
		name, text = rest[1:1+end], strings.TrimSpace(rest[end+2:])
	} else {
		var found bool
		name, text, found = strings.Cut(rest, " ")
		if !found {
			return "", "", false
		}
		text = strings.TrimSpace(text)
	}
	return name, text, name != "" && text != ""
}

func peerErrorText(err error) string {
	for _, known := range []error{ErrPeersDisabled, ErrPeerNotFound, ErrPeerAmbiguous, ErrPeerSelf, ErrPeerRefused, ErrPeerRateLimited, ErrPeerEmpty, ErrPeerTooLarge, ErrPeerNotHeld} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return err.Error()
}

func (c *Controller) argumentCommand(verb, args string) {
	if verb == "/feedback" {
		c.feedbackCommand(args)
		return
	}
	c.peerCommand(args)
}

// peerSettingsText only reports: the switches are written from Settings or the
// user file, because a slash command can arrive from a paired device that has
// no right to open this machine to its other sessions.
func (c *Controller) peerSettingsText() string {
	s := c.PeersSettings()
	state := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "messages between sessions: %s; dispatch to another project: %s", state(s.Enabled), state(s.Dispatch))
	if s.Pending {
		b.WriteString(" (applies when the next turn starts)")
	}
	fmt.Fprintf(&b, "\ndispatch ceilings: depth %s, open sessions %s (0 removes a ceiling)", strconv.Itoa(s.MaxDispatchDepth), strconv.Itoa(s.MaxDispatchedSessions))
	for _, r := range s.ReleaseProjects {
		fmt.Fprintf(&b, "\nreleased project: %s", r.Path)
	}
	fmt.Fprintf(&b, "\nchange them in Settings, or in [peers] of %s", s.Path)
	return b.String()
}
