//go:build !windows

package main

import "os"

// Windows verifies each executable with the OS Authenticode policy while the
// same read handle is held. Non-Windows builds keep the release-unit loader
// testable without importing platform-specific trust APIs.
var readVerifiedWindowsStagedPayloadFn = os.ReadFile
