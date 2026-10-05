// Package sessionv4 reads the session store Reasonix 1.x writes from 1.38.8:
// one directory per conversation under a project's sessions-v4 root, holding
// a manifest and an events.frames log of zstd-compressed JSON records grouped
// into checksummed batches, with large payloads and images in a shared
// content-addressed pool beside the root.
//
// It only reads. It takes no lock, writes no cache and never truncates a torn
// tail: 1.x owns every byte under the root, and a batch without its end record
// is simply not there yet. A conversation is the fold of its message events —
// complete, upsert, retract, history replace and legacy import — in order.
package sessionv4
