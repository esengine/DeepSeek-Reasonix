package sshfx

import (
	"bufio"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This string data is copied verbatim from https://filezilla-project.org/specs/draft-ietf-secsh-filexfer-13.txt
var fxStandardsText = `
SSH_FX_OK                            0
SSH_FX_EOF                           1
SSH_FX_NO_SUCH_FILE                  2
SSH_FX_PERMISSION_DENIED             3
SSH_FX_FAILURE                       4
SSH_FX_BAD_MESSAGE                   5
SSH_FX_NO_CONNECTION                 6
SSH_FX_CONNECTION_LOST               7
SSH_FX_OP_UNSUPPORTED                8
SSH_FX_INVALID_HANDLE                9
SSH_FX_NO_SUCH_PATH                  10
SSH_FX_FILE_ALREADY_EXISTS           11
SSH_FX_WRITE_PROTECT                 12
SSH_FX_NO_MEDIA                      13
SSH_FX_NO_SPACE_ON_FILESYSTEM        14
SSH_FX_QUOTA_EXCEEDED                15
SSH_FX_UNKNOWN_PRINCIPAL             16
SSH_FX_LOCK_CONFLICT                 17
SSH_FX_DIR_NOT_EMPTY                 18
SSH_FX_NOT_A_DIRECTORY               19
SSH_FX_INVALID_FILENAME              20
SSH_FX_LINK_LOOP                     21
SSH_FX_CANNOT_DELETE                 22
SSH_FX_INVALID_PARAMETER             23
SSH_FX_FILE_IS_A_DIRECTORY           24
SSH_FX_BYTE_RANGE_LOCK_CONFLICT      25
SSH_FX_BYTE_RANGE_LOCK_REFUSED       26
SSH_FX_DELETE_PENDING                27
SSH_FX_FILE_CORRUPT                  28
SSH_FX_OWNER_INVALID                 29
SSH_FX_GROUP_INVALID                 30
SSH_FX_NO_MATCHING_BYTE_RANGE_LOCK   31
`

func TestFxNames(t *testing.T) {
	whitespace := regexp.MustCompile(`[[:space:]]+`)

	scan := bufio.NewScanner(strings.NewReader(fxStandardsText))

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

		fx := Status(n)

		if got := fx.String(); got != name {
			t.Errorf("fx name mismatch for %d: got %q, but want %q", n, got, name)
		}
	}

	if err := scan.Err(); err != nil {
		t.Fatal("unexpected error:", err)
	}
}

func TestStatusIs(t *testing.T) {
	status := StatusFailure

	if !errors.Is(status, StatusFailure) {
		t.Error("errors.Is(StatusFailure, StatusFailure) != true")
	}
	if !errors.Is(status, &StatusPacket{StatusCode: StatusFailure}) {
		t.Error("errors.Is(StatusFailure, StatusPacket{StatusFailure}) != true")
	}
	if errors.Is(status, StatusOK) {
		t.Error("errors.Is(StatusFailure, StatusFailure) == true")
	}
	if errors.Is(status, &StatusPacket{StatusCode: StatusOK}) {
		t.Error("errors.Is(StatusFailure, StatusPacket{StatusFailure}) == true")
	}
}
