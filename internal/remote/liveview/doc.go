// Package liveview reduces the eventwire stream for the currently unfinished
// runtime Turn into a replayable semantic view.
//
// It deliberately has no event-count or byte truncation policy. Events which
// create distinct Desktop-visible items remain distinct, regardless of count.
// Potentially unbounded text is coalesced into its semantic object and remains
// subject to the Remote contentRef and per-object limits at the protocol edge.
package liveview
