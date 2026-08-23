package responses

import "reasonix/internal/provider"

func (c *client) canUseStatefulContinuation(messages []provider.Message, previousID, expectedDigest string) bool {
	if c.mode != "stateful" || previousID == "" || len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	if last.Role != provider.RoleUser || len(last.Images) > 0 {
		return false
	}
	return c.conversationDigest(messages[:len(messages)-1]) == expectedDigest
}
