package clipboard

import "os/exec"

func init() {
	if _, err := exec.LookPath("wl-paste"); err == nil {
		readCmd = []string{"wl-paste", "--no-newline"}
	} else if _, err := exec.LookPath("xclip"); err == nil {
		readCmd = []string{"xclip", "-selection", "clipboard", "-o"}
	} else if _, err := exec.LookPath("xsel"); err == nil {
		readCmd = []string{"xsel", "--clipboard", "--output"}
	}
	if _, err := exec.LookPath("wl-copy"); err == nil {
		writeCmd = []string{"wl-copy"}
	} else if _, err := exec.LookPath("xclip"); err == nil {
		writeCmd = []string{"xclip", "-selection", "clipboard"}
	} else if _, err := exec.LookPath("xsel"); err == nil {
		writeCmd = []string{"xsel", "--clipboard", "--input"}
	}
}
