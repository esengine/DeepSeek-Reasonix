// Package team runs the user's team of long-lived agents: it delivers what
// one agent sends another, routes each answer back, and keeps the handoff tree
// those messages form. It owns no session and imports no controller. It drives
// sessions through Panes, which the host implements over its runtimes, and
// learns how a turn ended only from the Settlement the host reports.
//
// Every message is an exchange in the teamroster book. The exchange the sender
// was answering when it sent is the new one's parent, so the book is a forest
// rooted at whatever the user asked. Three rules hold on that tree:
//
//   - An agent may not hand work to anyone upstream of it on its own branch;
//     that agent is waiting for this one, and a message back is a cycle.
//   - An answer is held while the agent that wrote it still has work out: its
//     children's replies come back to it as new turns serving the same
//     exchange, and only the turn that ends with nothing outstanding replies.
//   - How a turn ended is the host's word. A failed, stopped or interrupted
//     turn answers with that status and its code, never with an empty reply
//     the sender is left to explain.
//
// A message carries no authority. It reaches the receiver as framed data with
// a fixed caveat, and the approval-mode ceiling the caller hands in travels
// with it unchanged; the controller decides what that ceiling permits.
//
// The text the model reads is rendered here, deterministically: the same
// roster and exchange give the same bytes.
package team
