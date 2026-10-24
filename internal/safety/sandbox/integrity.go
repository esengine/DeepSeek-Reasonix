package sandbox

import (
	"errors"
	"runtime"
)

type IntegrityRequirement string

// CompleteFileIntegrity requires creation, overwrite, deletion and rename bounds.
const CompleteFileIntegrity IntegrityRequirement = "complete_file_mutations"

var ErrIncompleteIntegrity = errors.New("this execution requires a backend that confines creation, overwrite, deletion and rename; the current backend cannot provide that boundary")

// RequiredIntegrityFault is evaluated before grants or escape approval. A
// requirement belongs to the execution environment, rather than user approval.
func RequiredIntegrityFault(spec Spec) error {
	if spec.RequiredIntegrity == "" {
		return nil
	}
	if spec.RequiredIntegrity != CompleteFileIntegrity || !spec.Enforce() || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		return ErrIncompleteIntegrity
	}
	for _, root := range spec.ProtectedWriteRoots {
		if !outsideAll(root, confinedWriteDirs(spec)) {
			return ErrIncompleteIntegrity
		}
	}
	return nil
}

func refusesCommand(spec Spec) bool {
	return RequiredIntegrityFault(spec) != nil || (spec.RequiredIntegrity != "" && !AvailableFor(spec))
}
