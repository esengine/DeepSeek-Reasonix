package provider

// PresentedFile identifies a user-facing file deliverable. It deliberately
// stores no file bytes or authorization token; hosts revalidate it on access.
type PresentedFile struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

const PresentedFilesMetadataVersion = 1

// PresentedFilesMetadata is the durable, host-only envelope for explicit file
// deliverables. Versioning the optional record lets future readers fail closed
// or migrate without changing the provider-visible tool result text.
type PresentedFilesMetadata struct {
	Version int             `json:"version"`
	Files   []PresentedFile `json:"files"`
}

func NewPresentedFilesMetadata(files []PresentedFile) *PresentedFilesMetadata {
	if len(files) == 0 {
		return nil
	}
	return &PresentedFilesMetadata{
		Version: PresentedFilesMetadataVersion,
		Files:   append([]PresentedFile(nil), files...),
	}
}

func PresentedFileList(metadata *PresentedFilesMetadata) []PresentedFile {
	if metadata == nil || metadata.Version != PresentedFilesMetadataVersion {
		return nil
	}
	return append([]PresentedFile(nil), metadata.Files...)
}
