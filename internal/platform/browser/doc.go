// Package browser drives a Chromium-family browser the machine already has,
// for an agent that needs to see and operate web pages.
//
// The host owns the meaning of everything it hands the model. A page is shown
// as its accessibility tree with a ref on each element; refs are issued by this
// package, never reused, and retired when their document navigates away, so a
// stale one fails as browser.stale_ref instead of landing on whatever took its
// place. Inputs are real pointer and keyboard events, and a pointer input is
// refused as browser.covered when another element would receive it. Every
// failure carries a Code, and the detail beside it is for the reader.
//
// A Session is one agent's tabs. Sessions on the same profile share one
// browser process through a Pool, because a profile admits one process at a
// time. The browser runs on a profile of its own, never the person's everyday
// one, and is driven over --remote-debugging-pipe where the platform lets a
// child inherit the pipe descriptors; elsewhere (Windows) it listens on a
// loopback port, which any local process can reach while it runs.
//
// checkURL bounds what a page may be: http and https, about:blank, and files
// inside the workspace roots once symlinks resolve. A page reached by a link
// past that boundary is refused when it is read or acted on.
package browser
