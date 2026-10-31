package teamroster

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var (
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 32)...)
	jpegBytes = append([]byte("\xff\xd8\xff\xe0"), bytes.Repeat([]byte{2}, 32)...)
	gifBytes  = append([]byte("GIF89a"), bytes.Repeat([]byte{3}, 32)...)
	webpBytes = append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{4}, 32)...)
)

func TestAPhotoIsStoredByWhatItIsAndOnce(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for ext, data := range map[string][]byte{".png": pngBytes, ".jpg": jpegBytes, ".gif": gifBytes, ".webp": webpBytes} {
		name, err := s.SaveAvatar(data)
		if err != nil || filepath.Ext(name) != ext || !avatarFile.MatchString(name) {
			t.Fatalf("SaveAvatar(%s) = %q, %v", ext, name, err)
		}
		again, err := s.SaveAvatar(data)
		if err != nil || again != name {
			t.Fatalf("the same picture saved twice is %q then %q (%v)", name, again, err)
		}
		path, err := s.AvatarPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
			t.Fatalf("%s holds %d bytes, want the %d saved", name, len(got), len(data))
		}
	}
}

func TestAPhotoThatIsNotAPictureOrTooLargeIsRefused(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.SaveAvatar([]byte("<svg onload=alert(1)>")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an svg = %v, want ErrInvalid", err)
	}
	big := append(append([]byte{}, pngBytes...), make([]byte, MaxAvatarBytes)...)
	if _, err := s.SaveAvatar(big); !errors.Is(err, ErrTooLong) {
		t.Fatalf("an oversize picture = %v, want ErrTooLong", err)
	}
}

func TestAPhotoNameCannotReachOutsideTheAvatarFolder(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, name := range []string{"../roster.json", "a.png", "0123456789abcdef.svg", "/etc/passwd"} {
		if _, err := s.AvatarPath(name); !errors.Is(err, ErrInvalid) {
			t.Fatalf("AvatarPath(%q) = %v, want ErrInvalid", name, err)
		}
	}
}

func TestARosterOnlyNamesAStoredPhotoShape(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	_, err := s.Update(context.Background(), func(r *Roster) error {
		r.AddAgent(Agent{Name: "Ada", Main: true, Avatar: Avatar{Image: "../../x.png"}, CreatedBy: ByUser})
		return nil
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("a roster naming a path as a photo = %v, want ErrInvalid", err)
	}
}

func TestPruneKeepsOnlyThePhotosAgentsStillWear(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	worn, _ := s.SaveAvatar(pngBytes)
	dropped, _ := s.SaveAvatar(gifBytes)
	r := Roster{Agents: []Agent{{Avatar: Avatar{Image: worn}}}}
	if err := s.PruneAvatars(r); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.AvatarPath(worn); !exists(p) {
		t.Fatal("pruning removed a photo an agent wears")
	}
	if p, _ := s.AvatarPath(dropped); exists(p) {
		t.Fatal("pruning kept a photo nobody wears")
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
