package sshfx

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This string data is copied verbatim from https://filezilla-project.org/specs/draft-ietf-secsh-filexfer-13.txt
// except where commented that it was taken from a different source.
var fxpStandardsText = `
SSH_FXP_INIT                1
SSH_FXP_VERSION             2
SSH_FXP_OPEN                3
SSH_FXP_CLOSE               4
SSH_FXP_READ                5
SSH_FXP_WRITE               6
SSH_FXP_LSTAT               7
SSH_FXP_FSTAT               8
SSH_FXP_SETSTAT             9
SSH_FXP_FSETSTAT           10
SSH_FXP_OPENDIR            11
SSH_FXP_READDIR            12
SSH_FXP_REMOVE             13
SSH_FXP_MKDIR              14
SSH_FXP_RMDIR              15
SSH_FXP_REALPATH           16
SSH_FXP_STAT               17
SSH_FXP_RENAME             18
SSH_FXP_READLINK           19
SSH_FXP_SYMLINK            20 // Deprecated in filexfer-13 added from filexfer-02
SSH_FXP_LINK               21
SSH_FXP_BLOCK              22
SSH_FXP_UNBLOCK            23

SSH_FXP_STATUS            101
SSH_FXP_HANDLE            102
SSH_FXP_DATA              103
SSH_FXP_NAME              104
SSH_FXP_ATTRS             105

SSH_FXP_EXTENDED          200
SSH_FXP_EXTENDED_REPLY    201
`

func TestFxpNames(t *testing.T) {
	whitespace := regexp.MustCompile(`[[:space:]]+`)

	scan := bufio.NewScanner(strings.NewReader(fxpStandardsText))

	for scan.Scan() {
		line := scan.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := whitespace.Split(line, 2)
		if len(fields) < 2 {
			t.Fatalf("unexpected standards text line: %q", line)
		}

		name, value := fields[0], fields[1]
		n, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		fxp := PacketType(n)

		if got := fxp.String(); got != name {
			t.Errorf("fxp name mismatch for %d: got %q, but want %q", n, got, name)
		}
	}

	if err := scan.Err(); err != nil {
		t.Fatal("unexpected error:", err)
	}
}
