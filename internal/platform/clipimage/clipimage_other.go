//go:build !darwin && !windows

package clipimage

import (
	"context"
	"os/exec"
	"strings"
)

// imageTypes are the image formats a paste can carry, most preferred first.
var imageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

type tool struct {
	name string
	list []string
	get  func(mime string) []string
}

var tools = []tool{
	{"wl-paste", []string{"--list-types"}, func(mime string) []string { return []string{"--type", mime, "--no-newline"} }},
	{"xclip", []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}, func(mime string) []string {
		return []string{"-selection", "clipboard", "-t", mime, "-o"}
	}},
}

// read asks whichever clipboard tool is installed what the clipboard offers
// and takes the first image type on the list. A tool that cannot say is taken
// to be offering no image, which sends the paste to text.
func read(ctx context.Context) ([]byte, error) {
	for _, t := range tools {
		path, err := exec.LookPath(t.name)
		if err != nil {
			continue
		}
		offered, err := run(ctx, path, t.list...)
		if err != nil {
			continue
		}
		for _, mime := range imageTypes {
			if listed(offered, mime) {
				raw, err := run(ctx, path, t.get(mime)...)
				if err == nil && len(raw) > 0 {
					return raw, nil
				}
			}
		}
	}
	return nil, ErrNoImage
}

func listed(offered []byte, mime string) bool {
	for f := range strings.FieldsSeq(string(offered)) {
		if strings.EqualFold(f, mime) {
			return true
		}
	}
	return false
}
