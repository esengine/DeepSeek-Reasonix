// Package packagegrant removes, from the tree an application runs from, the
// access entries that grant a specific Windows app package.
//
// Chromium's sandboxed renderer exits with STATUS_BREAKPOINT while loading a DLL
// whose DACL carries an allow entry for a specific AppContainer package SID, and
// the window never paints. On Electron 43 and 44 alike one such entry on
// ffmpeg.dll is enough, and a live package's SID fails the same as one whose
// profile is gone; the same entry on the executable, on a directory alone, or as
// a deny entry changes nothing. The built-in package groups (ALL APPLICATION
// PACKAGES, ALL RESTRICTED APPLICATION PACKAGES) and capability SIDs do not.
//
// Such entries arrive from whatever last ran an AppContainer against files in
// that tree with inheritance on: a sandbox granting its package read and
// execute on the directory of a tool it launches is the ordinary way.
//
// The judgement reads the SID's structure — authority 15, base RID 2, more
// sub-authorities than a built-in group has — never an account name, which a
// package SID does not resolve to anyway. Only allow entries are removed, so a
// pass narrows access and never widens it. An entry inherited from above the
// tree cannot be removed at its source without touching a directory this tree
// does not own; the object that inherits it is protected instead, keeping every
// other entry it had as its own.
package packagegrant
