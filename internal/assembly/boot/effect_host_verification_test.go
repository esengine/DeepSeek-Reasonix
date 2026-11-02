package boot

import ()

// holdChangedTurns is the user-level opt-in that makes a turn which changed files
// owe its check, review and sign-off; tests of what a turn owes start from it.
const holdChangedTurns = "\n[agent]\nhost_verification = \"on\"\n"
