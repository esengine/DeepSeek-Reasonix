// Package process is the shared vocabulary for work the kernel is running: a
// closed set of states and the typed causes that explain them.
//
// It records nothing and owns no lifecycle. Each owner keeps its own states and
// transitions and maps them onto this set; a layer above may project a class,
// never re-derive one. A cause is left empty when the owner does not know it.
package process
