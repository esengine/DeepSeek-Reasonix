// Package terminal runs the shells a person opens for themselves in the
// workbench.
//
// A terminal is not the agent's: its commands are the person's own, run as them
// with no model, permission or sandbox in between, and the model sees none of its
// output unless the person sends a selection to the conversation. That boundary
// is why the package knows nothing of sessions or tools.
//
// A Session owns one pseudo-terminal. Its output is kept in a bounded ring so a
// window that reconnects, or opens after the shell began, replays what it
// missed; a process that exits is kept, with its code, until the person closes
// it. Every failure carries a Code.
package terminal
