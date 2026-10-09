// Package workspacelist owns the file holding the sidebar's remembered
// projects. Serve edits it per request and the storage relocation repair merges
// an older copy into it, so reading, locked atomic updates and the one-time
// merge live here rather than in either of them.
package workspacelist
