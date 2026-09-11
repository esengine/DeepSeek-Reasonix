# Reasonix systray patches

This directory is copied from `fyne.io/systray` version
`v1.12.3-0.20260814134402-f60f01be81c6`.

Reasonix adds `SetIconID` on Windows and registers the notification icon with
`NIF_GUID`. Signed builds pass a deterministic data-home GUID before `Run`, which lets Windows
keep the user's notification-area visibility preference when the signed desktop
executable moves between `versions/<version>/` directories.

When updating the upstream copy, preserve the `SetIconID` API and ensure the
configured GUID is included in every `Shell_NotifyIcon` add, modify, delete, and
Explorer restart operation.

Unsigned/development builds retain the window-and-ID identity. If initial GUID
registration fails, retry without NIF_GUID and retain that mode for the process
lifetime, including Explorer restarts. Never delete the rejected GUID because
another process may own it. Each independent data home gets a different GUID.
