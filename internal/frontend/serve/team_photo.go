package serve

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"reasonix/internal/state/teamroster"
)

// A data URL carries a picture as base64, a third larger than the picture.
const photoBodyLimit = teamroster.MaxAvatarBytes*4/3 + 4<<10

var errNotDataURL = &teamroster.FieldError{Field: "avatar.image", Err: teamroster.ErrInvalid}

// decodePhoto reads a picture out of a data URL. The type it declares is not
// trusted: the store names the picture by its own bytes.
func decodePhoto(url string) ([]byte, error) {
	head, payload, ok := strings.Cut(url, ";base64,")
	if !ok || !strings.HasPrefix(head, "data:image/") {
		return nil, errNotDataURL
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, errNotDataURL
	}
	return data, nil
}

// setPhoto saves the picture the window cropped and puts it on the agent.
func (t *hubTeam) setPhoto(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, photoBodyLimit)
	var in struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			teamError(w, &teamroster.FieldError{Field: "avatar.image", Err: teamroster.ErrTooLong})
			return
		}
		badBody(w)
		return
	}
	data, err := decodePhoto(in.Image)
	if err != nil {
		teamError(w, err)
		return
	}
	t.wearPhoto(w, r, data)
}

func (t *hubTeam) clearPhoto(w http.ResponseWriter, r *http.Request) { t.wearPhoto(w, r, nil) }

// wearPhoto puts name on the agent, empty taking the photo off, and drops the
// pictures nobody wears any more.
func (t *hubTeam) wearPhoto(w http.ResponseWriter, r *http.Request, data []byte) {
	if err := t.store.SetAgentPhoto(r.Context(), r.PathValue("id"), data); err != nil {
		teamError(w, err)
		return
	}
	t.changed()
	t.writeView(w)
}

// avatar serves a stored photo. Its name is content-addressed, so a name once
// served never changes what it shows.
func (t *hubTeam) avatar(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	path, err := t.store.AvatarPath(name)
	if err != nil {
		teamError(w, err)
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		refuse(w, http.StatusNotFound, "team.unknown_photo", "no such photo", nil)
		return
	}
	if err != nil {
		teamError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentTypeOf(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// photoURL is where the window reads a stored photo; empty when there is none.
func photoURL(name string) string {
	if name == "" {
		return ""
	}
	return "/team/avatars/" + name
}
