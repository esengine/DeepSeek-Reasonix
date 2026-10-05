package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"fmt"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/traystate"
)

// TrayPrefs is what a window can be asked about its own status icon. Icon is
// what the setting asks for; Live is whether this launch has one, because a
// platform that gives an icon up once cannot be handed another. The two differ
// only until the next launch, and a panel showing both can say so.
type TrayPrefs struct {
	Icon        bool `json:"icon"`
	Live        bool `json:"live"`
	CloseToTray bool `json:"closeToTray"`
}

// TrayState is the fold the icon paints: a projection of what the panes are
// doing, recomputed on every read. Losing one costs nothing the next read does
// not restore, which is why it is pulled rather than carried on the stream.
type TrayState struct {
	Mood      string     `json:"mood"`
	Panes     int        `json:"panes"`
	Working   int        `json:"working"`
	Attention int        `json:"attention"`
	Jobs      int        `json:"jobs"`
	Busy      bool       `json:"busy"`
	Line      string     `json:"line"`
	Labels    TrayLabels `json:"labels"`
}

// TrayLabels is what the menu under the icon says. It rides the state because
// the icon is one surface: a shell painting it should not also have to carry a
// catalogue, and the language is the desktop's, not the kernel's.
type TrayLabels struct {
	Open        string `json:"open"`
	CloseToTray string `json:"closeToTray"`
	Quit        string `json:"quit"`
}

// TrayLine says the fold in the reader's language. Exported so a host painting
// its own icon and a client reading this state get one sentence rather than two
// that drift. Background jobs are the half nobody would guess: a hidden window
// with a dev server in it is still doing something on the machine's behalf.
func TrayLine(say i18n.Messages, fold traystate.State) string {
	var parts []string
	if fold.Attention > 0 {
		parts = append(parts, fmt.Sprintf(say.TrayAttention, fold.Attention))
	}
	if fold.Working > 0 {
		parts = append(parts, fmt.Sprintf(say.TrayWorking, fold.Working))
	}
	if fold.Jobs > 0 {
		parts = append(parts, fmt.Sprintf(say.TrayJobs, fold.Jobs))
	}
	if len(parts) == 0 {
		return say.TrayIdle
	}
	return strings.Join(parts, " · ")
}

// TrayHost is what only a window can answer. The hub fills Jobs itself, from
// the panes it holds; everything else here is the window's own. A kernel with
// no window registers none of these routes, which is what a client with no
// icon reads as "there is no tray here".
type TrayHost interface {
	IconLive() bool
	TrayFold() traystate.State
	ApplyTrayPrefs(TrayPrefs)
}

const codeTrayRejected = "tray.rejected"

func moodName(m traystate.Mood) string {
	switch m {
	case traystate.MoodAttention:
		return "attention"
	case traystate.MoodWorking:
		return "working"
	default:
		return "idle"
	}
}

func (h *Hub) registerTrayRoutes(mux *http.ServeMux) {
	if h.opts.Tray == nil {
		return
	}
	mux.HandleFunc("GET /tray/prefs", h.readTrayPrefs)
	mux.HandleFunc("PUT /tray/prefs", h.writeTrayPrefs)
	mux.HandleFunc("GET /tray/state", h.readTrayState)
}

func (h *Hub) readTrayPrefs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, h.TrayPrefs())
}

// TrayPrefs reads the durable answer. Exported for a host whose own menu shows
// the same switches: one reader, whichever surface is asking.
func (h *Hub) TrayPrefs() TrayPrefs {
	if h.opts.Tray == nil {
		return TrayPrefs{}
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	return TrayPrefs{
		Icon:        cfg.DesktopTray() != "off",
		Live:        h.opts.Tray.IconLive(),
		CloseToTray: cfg.DesktopClosesToBackground(),
	}
}

// SetTrayPrefs persists both switches and answers with what they became.
// Exported so a host's own menu offering the same switch runs this rather than
// a second copy of it — there is one "close to tray", whichever control asked.
func (h *Hub) SetTrayPrefs(icon, closeToTray bool) (TrayPrefs, error) {
	if h.opts.Tray == nil {
		return TrayPrefs{}, errors.New("this kernel has no window to put an icon on")
	}
	// No icon, no backgrounding. A hidden window with nothing to bring it back
	// is the one state this must never produce, so the answer depends on an
	// icon being both asked for and actually up.
	closeToTray = closeToTray && icon && h.opts.Tray.IconLive()

	trayMode, behavior := "auto", "quit"
	if !icon {
		trayMode = "off"
	}
	if closeToTray {
		behavior = "background"
	}
	path := config.UserConfigPath()
	edit := config.LoadForEdit(path)
	if err := edit.SetDesktopTray(trayMode); err != nil {
		return TrayPrefs{}, err
	}
	if err := edit.SetDesktopCloseBehavior(behavior); err != nil {
		return TrayPrefs{}, err
	}
	if err := edit.SaveTo(path); err != nil {
		return TrayPrefs{}, err
	}
	prefs := h.TrayPrefs()
	// The window acts on it now rather than reading it back on a timer: the
	// close button has to answer what was just asked for.
	h.opts.Tray.ApplyTrayPrefs(prefs)
	return prefs, nil
}

func (h *Hub) writeTrayPrefs(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Icon        bool `json:"icon"`
		CloseToTray bool `json:"closeToTray"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	prefs, err := h.SetTrayPrefs(body.Icon, body.CloseToTray)
	if err != nil {
		refuse(w, http.StatusConflict, codeTrayRejected, err.Error(), nil)
		return
	}
	writeJSON(w, prefs)
}

func (h *Hub) readTrayState(w http.ResponseWriter, _ *http.Request) {
	fold := h.opts.Tray.TrayFold()
	fold.Jobs = h.runningJobs()
	say := i18n.CatalogFor(config.LoadForEdit(config.UserConfigPath()).DesktopLanguage())
	writeJSON(w, TrayState{
		Mood:      moodName(fold.Mood()),
		Panes:     fold.Panes,
		Working:   fold.Working,
		Attention: fold.Attention,
		Jobs:      fold.Jobs,
		Busy:      fold.Busy(),
		Line:      TrayLine(say, fold),
		Labels:    TrayLabels{Open: say.TrayOpen, CloseToTray: say.TrayCloseToTray, Quit: say.TrayQuit},
	})
}

// runningJobs counts what this machine's own panes would leave behind. A remote
// pane's jobs live on the far kernel, and a number answered from here could not
// be acted on from this window.
func (h *Hub) runningJobs() int {
	running := 0
	for _, rt := range h.localRuntimes() {
		if rt.Server != nil {
			running += len(rt.Server.Controller().Jobs())
		}
	}
	return running
}
