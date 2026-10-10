// Package wsl discovers the WSL distributions on a Windows machine and probes
// one before anything is installed in it. A distribution is a place a kernel
// can run, like an SSH host; everything here is read-only and launches
// wsl.exe from System32 with a fixed argv, never through a shell.
//
// Discovery reads the structure of `wsl -l -v` (a header line, an optional
// default marker, a name, a state word, a version number) rather than its
// words, because the state words and the header are localized.
package wsl
