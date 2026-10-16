package boot

import (
	"runtime"
	"strings"
)

// platformNote tells the model which surface it is running on.
//
// The environment section already lists tools and an OS string, and it stays
// exactly as it is: it describes the machine. What it cannot say is the one
// thing that changes how a task should be planned — that the machine is a phone
// in someone's hand rather than a desk. No window to resize, no physical
// keyboard, answers read on a small screen, and a workspace that may be a
// folder only this app can open.
//
// One short paragraph, because everything longer is a cost paid on every turn:
// this rides the cache-stable prefix. Kept to a fixed string chosen at build
// time by GOOS, so it never diverges mid-session and never rewrites the bytes
// the prefix cache keys on.
func platformNote() string {
	if runtime.GOOS != "android" {
		return ""
	}
	return strings.TrimSpace(`
## This device

You are running on an Android phone, inside the Reasonix mobile app — not on a desktop.

- The interface is a touch screen. There is no hover and no window to resize, and "open a browser" means an app on this phone.
- Text is entered with a soft keyboard, and the person sees your messages rather than the steps between them.
- The workspace may be app-private storage, or a folder on this phone opened through the "all files access" permission. A path outside it may not be readable.
- Prefer short answers: a phone screen is small, so a long reply is one that gets scrolled past.`)
}
