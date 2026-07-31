package sftp

import (
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	typeDirectory = "d"
	typeFile      = "[^d]"
)

func TestRunLsWithExamplesDirectory(t *testing.T) {
	path := "examples"
	item, _ := os.Stat(path)
	result := runLs(nil, item)
	runLsTestHelper(t, result, typeDirectory, path)
}

func TestRunLsWithSourceFile(t *testing.T) {
	path := "ls_formatting_test.go"
	item, _ := os.Stat(path)
	result := runLs(nil, item)
	runLsTestHelper(t, result, typeFile, path)
}

func TestRunLsWithExamplesDirectoryWithOSLookup(t *testing.T) {
	path := "examples"
	item, _ := os.Stat(path)
	result := runLs(osIDLookup{}, item)
	runLsTestHelper(t, result, typeDirectory, path)
}

func TestRunLsWithSourceFileWithOSLookup(t *testing.T) {
	path := "ls_formatting_test.go"
	item, _ := os.Stat(path)
	result := runLs(osIDLookup{}, item)
	runLsTestHelper(t, result, typeFile, path)
}

/*
The format of the `longname' field is unspecified by this protocol.
It MUST be suitable for use in the output of a directory listing
command (in fact, the recommended operation for a directory listing
command is to simply display this data).  However, clients SHOULD NOT
attempt to parse the longname field for file attributes; they SHOULD
use the attrs field instead.

	The recommended format for the longname field is as follows:

	    -rwxr-xr-x   1 mjos     staff      348911 Mar 25 14:29 t-filexfer
	    1234567890 123 12345678 12345678 12345678 123456789012

Here, the first line is sample output, and the second field indicates
widths of the various fields.  Fields are separated by spaces.  The
first field lists file permissions for user, group, and others; the
second field is link count; the third field is the name of the user
who owns the file; the fourth field is the name of the group that
owns the file; the fifth field is the size of the file in bytes; the
sixth field (which actually may contain spaces, but is fixed to 12
characters) is the file modification time, and the seventh field is
the file name.  Each field is specified to be a minimum of certain
number of character positions (indicated by the second line above),
but may also be longer if the data does not fit in the specified
length.

	The SSH_FXP_ATTRS response has the following format:

	    uint32     id
	    ATTRS      attrs

where `id' is the request identifier, and `attrs' is the returned
file attributes as described in Section “File Attributes”.

N.B.: FileZilla does parse this ls formatting, and so not rendering it
on any particular GOOS/GOARCH can cause compatibility issues with this client.
*/
func runLsTestHelper(t *testing.T, result, expectedType, path string) {
	// using regular expressions to make tests work on all systems
	// a virtual file system (like afero) would be needed to mock valid filesystem checks
	// expected layout is:
	// drwxr-xr-x   8 501      20            272 Aug  9 19:46 examples

	t.Log(result)

	sparce := strings.Split(result, " ")

	var fields []string
	for _, field := range sparce {
		if field == "" {
			continue
		}

		fields = append(fields, field)
	}

	perms, linkCnt, user, group, size := fields[0], fields[1], fields[2], fields[3], fields[4]
	dateTime := strings.Join(fields[5:8], " ")
	filename := fields[8]

	if runtime.GOOS == "zos" {
		// User and Group are always only uppercase characters on z/OS
		user = strings.ToLower(user)
		group = strings.ToLower(group)
	}

	// permissions (len 10, "drwxr-xr-x")
	const (
		rwxs = "[-r][-w][-xsS]"
		rwxt = "[-r][-w][-xtT]"
	)
	if ok, err := regexp.MatchString("^"+expectedType+rwxs+rwxs+rwxt+"$", perms); !ok {
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		t.Errorf("runLs(%q): permission field mismatch, expected dir, got: %#v, err: %#v", path, perms, err)
	}

	// link count (len 3, number)
	const (
		number = "(?:[0-9]+)"
	)
	if ok, err := regexp.MatchString("^"+number+"$", linkCnt); !ok {
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		t.Errorf("runLs(%q): link count field mismatch, got: %#v, err: %#v", path, linkCnt, err)
	}

	// username / uid (len 8, number or string)
	const (
		name = "(?:[A-Za-z_][-A-Z.a-z0-9_]*)"
	)
	if ok, err := regexp.MatchString("^(?:"+number+"|"+name+")+$", user); !ok {
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		t.Errorf("runLs(%q): username / uid mismatch, expected user, got: %#v, err: %#v", path, user, err)
	}

	// groupname / gid (len 8, number or string)
	if ok, err := regexp.MatchString("^(?:"+number+"|"+name+")+$", group); !ok {
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		t.Errorf("runLs(%q): groupname / gid mismatch, expected group, got: %#v, err: %#v", path, group, err)
	}

	// filesize (len 8)
	if ok, err := regexp.MatchString("^"+number+"$", size); !ok {
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		t.Errorf("runLs(%q): filesize field mismatch, expected size in bytes, got: %#v, err: %#v", path, size, err)
	}

	// mod time (len 12, e.g. Aug  9 19:46)
	_, err := time.Parse("Jan 2 15:04", dateTime)
	if err != nil {
		_, err = time.Parse("Jan 2 2006", dateTime)
		if err != nil {
			t.Errorf("runLs.dateTime = %#v should match `Jan 2 15:04` or `Jan 2 2006`: %+v", dateTime, err)
		}
	}

	// filename
	if path != filename {
		t.Errorf("runLs.filename = %#v, expected: %#v", filename, path)
	}
}
