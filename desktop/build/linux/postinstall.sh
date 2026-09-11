#!/bin/sh
# Refresh desktop integration after both fresh installs and upgrades. Debian's
# package triggers usually do this too, but an explicit best-effort refresh
# repairs stale blank icons on desktops that kept an older cache.

# Chromium's SUID sandbox helper must be root:root 4755 so the renderer sandbox
# works without launching the shell with --no-sandbox.
if [ -f /usr/lib/reasonix/app/chrome-sandbox ]; then
	chown root:root /usr/lib/reasonix/app/chrome-sandbox || true
	chmod 4755 /usr/lib/reasonix/app/chrome-sandbox || true
fi

if [ -x /usr/bin/gtk-update-icon-cache ]; then
	/usr/bin/gtk-update-icon-cache --force --quiet /usr/share/icons/hicolor || true
fi

if [ -x /usr/bin/update-desktop-database ]; then
	/usr/bin/update-desktop-database --quiet /usr/share/applications || true
fi

exit 0
