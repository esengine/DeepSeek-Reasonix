// Package computer operates other applications on this machine for an agent,
// through a native helper that reads their accessibility trees, captures their
// windows, and puts input into them: Swift on macOS, C++ on Windows, one
// protocol for both.
//
// The helper decides nothing; this package owns what the model is shown and
// what it may reach. An application is named by its bundle id on macOS, and on
// Windows by its process file name or, for a Store application, its package
// family name. Some are never operated at all: this window, terminals, the
// keychain, password managers and system settings, where an agent's input
// would reach past every other boundary the host keeps. Refs are the helper's,
// issued once per element and never reused. A click at a point is placed in
// the pixels of the application's latest screenshot and converted to screen
// coordinates here, so the model never does display arithmetic.
//
// A click is an accessibility action on the element under the point, shown by
// the helper's own cursor, and an element that has no such action answers
// computer.no_action rather than being clicked some other way. On macOS typing
// goes to the application where it stands; Windows delivers keys only to the
// foreground, so there typing, keys and paste bring the application forward
// first. Escape pressed while the helper's cursor is on screen stops the run
// between steps.
package computer
