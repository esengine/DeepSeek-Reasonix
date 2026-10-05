package clipimage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// noImage is what the script answers when the clipboard lacks the class.
const noImage = "__REASONIX_NO_CLIPBOARD_IMAGE__"

// read asks AppleScript for the clipboard as PNG, then JPEG. AppleScript can
// only hand binary data over by writing it to a file.
func read(ctx context.Context) ([]byte, error) {
	for _, class := range []string{"PNGf", "JPEG"} {
		raw, err := readClass(ctx, class)
		if !errors.Is(err, ErrNoImage) {
			return raw, err
		}
	}
	return nil, ErrNoImage
}

func readClass(ctx context.Context, class string) ([]byte, error) {
	f, err := os.CreateTemp("", "reasonix-clip-*.bin")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)
	script := fmt.Sprintf(`
set hasImageType to false
repeat with typeEntry in (clipboard info)
	if (item 1 of typeEntry) is «class %s» then
		set hasImageType to true
		exit repeat
	end if
end repeat
if not hasImageType then return %q
set img to the clipboard as «class %s»
set f to open for access (POSIX file %q) with write permission
try
	set eof f to 0
	write img to f
	close access f
on error errMsg
	try
		close access f
	end try
	error errMsg
end try`, class, noImage, class, path)
	out, err := run(ctx, "osascript", "-e", script)
	if err != nil {
		return nil, fmt.Errorf("read clipboard image: %w", err)
	}
	if strings.TrimSpace(string(out)) == noImage {
		return nil, ErrNoImage
	}
	return os.ReadFile(path)
}
