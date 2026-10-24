package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"sync"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

// CheckInputs records what the host could observe at the actual launch
// boundary. Scope gaps are explicit; a successful check is not a declaration
// that arbitrary external inputs have been isolated or observed.
type CheckInputs struct {
	Before             ContentSnapshot         `json:"before"`
	After              ContentSnapshot         `json:"after"`
	Executable         fileutil.ContentVersion `json:"executable"`
	Interpreter        fileutil.ContentVersion `json:"interpreter"`
	ArgumentsDigest    string                  `json:"arguments_digest"`
	CommandDigest      string                  `json:"command_digest"`
	EnvironmentHash    string                  `json:"environment_hash"`
	Directory          string                  `json:"directory"`
	Platform           string                  `json:"platform"`
	Completed          bool                    `json:"completed"`
	Stable             bool                    `json:"stable"`
	Gaps               []string                `json:"gaps"`
	EnvironmentReceipt string                  `json:"environment_receipt,omitempty"`
}

// CheckInputObserver is private to one host invocation. Hooks run before it is
// installed. Commands, credentials and environment values never enter receipts.
type CheckInputObserver struct {
	root    string
	mu      sync.Mutex
	inputs  []CheckInputs
	pending int
}

func NewCheckInputObserver(root string) *CheckInputObserver { return &CheckInputObserver{root: root} }

func (o *CheckInputObserver) Begin(ctx context.Context, launch tool.ShellLaunch) (func(bool), error) {
	input := CheckInputs{Directory: launch.Directory, Platform: runtime.GOOS + "/" + runtime.GOARCH,
		ArgumentsDigest: inputDigest(launch.Arguments), EnvironmentHash: inputDigest(launch.Environment),
		CommandDigest: CheckCommandDigest(launch.Command),
		Gaps:          []string{"git_metadata_unobserved", "external_inputs_unobserved"}}
	input.Before = CaptureContentSnapshot(ctx, o.root, 50_000)
	input.Executable = captureExecutable(ctx, launch.Executable, launch.Directory)
	input.Interpreter = captureExecutable(ctx, launch.Interpreter, launch.Directory)
	o.mu.Lock()
	index := len(o.inputs)
	o.inputs = append(o.inputs, input)
	o.pending++
	o.mu.Unlock()
	var once sync.Once
	return func(completed bool) {
		once.Do(func() {
			input.Completed = completed
			if completed && ctx.Err() == nil {
				input.After = CaptureContentSnapshot(ctx, o.root, 50_000)
				input.Stable = input.Before.Complete && input.After.Complete && input.Before.Digest == input.After.Digest &&
					input.Executable.Kind == "file" && input.Interpreter.Kind == "file" &&
					input.Executable == captureExecutable(ctx, launch.Executable, launch.Directory) &&
					input.Interpreter == captureExecutable(ctx, launch.Interpreter, launch.Directory)
			}
			o.mu.Lock()
			o.inputs[index] = input
			o.pending--
			o.mu.Unlock()
		})
	}, nil
}

func (o *CheckInputObserver) Inputs() []CheckInputs {
	o.mu.Lock()
	defer o.mu.Unlock()
	result := slices.Clone(o.inputs)
	for i := range result {
		result[i].Gaps = slices.Clone(result[i].Gaps)
	}
	return result
}

func captureExecutable(ctx context.Context, path, directory string) fileutil.ContentVersion {
	if path == "" {
		return fileutil.ContentVersion{}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fileutil.ContentVersion{}
	}
	version, err := fileutil.ReadContentVersion(ctx, resolved)
	if err != nil {
		return fileutil.ContentVersion{}
	}
	return version
}

func inputDigest(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func CheckCommandDigest(command string) string {
	if command == "" {
		return ""
	}
	return inputDigest(command)
}

// HasLaunchIdentity validates the observable launch fields; it deliberately
// says nothing about external-input coverage or the check's exit status.
func (i CheckInputs) HasLaunchIdentity(directory string) bool {
	return i.Directory == directory && directory != "" && i.Platform == runtime.GOOS+"/"+runtime.GOARCH &&
		i.Executable.Kind == "file" && validInputDigest(i.Executable.Digest) &&
		i.Interpreter.Kind == "file" && validInputDigest(i.Interpreter.Digest) &&
		validInputDigest(i.ArgumentsDigest) && validInputDigest(i.CommandDigest) && validInputDigest(i.EnvironmentHash)
}

func validInputDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
