package protocol

type FileEntry struct {
	Name  string `json:"name" validate:"nonempty"`
	Path  string `json:"path" validate:"relativePath"`
	IsDir bool   `json:"isDir"`
}

type FileListParams struct {
	RuntimeQuery
	Path   string `json:"path" validate:"relativePath"`
	Cursor Cursor `json:"cursor,omitempty"`
	Limit  *int   `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type FileListResult struct {
	Entries    []FileEntry `json:"entries"`
	HasMore    bool        `json:"hasMore"`
	NextCursor Cursor      `json:"nextCursor,omitempty"`
}

type FileSearchParams struct {
	RuntimeQuery
	Query string `json:"query" validate:"nonempty"`
	Limit *int   `json:"limit,omitempty" validate:"min=1,max=100"`
}

type FileSearchResult struct {
	Entries          []FileEntry            `json:"entries"`
	Truncated        bool                   `json:"truncated"`
	TruncationReason SearchTruncationReason `json:"truncationReason,omitempty"`
	ReturnedItems    int                    `json:"returnedItems" validate:"min=0,max=100"`
	TotalItems       *int                   `json:"totalItems,omitempty" validate:"min=0"`
}

type FilePreviewParams struct {
	RuntimeQuery
	Path string `json:"path" validate:"relativePath,nonempty"`
}

type FilePreviewResult struct {
	Name             string               `json:"name" validate:"nonempty"`
	Path             string               `json:"path" validate:"relativePath,nonempty"`
	Kind             FileKind             `json:"kind"`
	SizeBytes        int64                `json:"sizeBytes" validate:"min=0"`
	ReturnedBytes    int64                `json:"returnedBytes" validate:"min=0,max=262144"`
	Binary           bool                 `json:"binary"`
	Truncated        bool                 `json:"truncated"`
	TruncationReason ByteTruncationReason `json:"truncationReason,omitempty"`
	Body             *string              `json:"body,omitempty"`
}

func (r FilePreviewResult) Validate() error {
	if r.Kind == FileText {
		if r.Binary || r.Body == nil || r.ReturnedBytes > r.SizeBytes {
			return validationError("text preview has inconsistent binary or byte fields")
		}
		if r.Truncated != (r.SizeBytes > r.ReturnedBytes) {
			return validationError("text preview truncated must match returned bytes")
		}
		if r.Truncated != (r.TruncationReason == ByteLimit) {
			return validationError("text preview truncation must use byte_limit")
		}
	} else if !r.Binary || r.ReturnedBytes != 0 || r.Body != nil || r.Truncated || r.TruncationReason != "" {
		return validationError("binary, image, and pdf previews are metadata only")
	}
	return nil
}

type WorkspaceChangesParams struct {
	RuntimeQuery
	Cursor Cursor `json:"cursor,omitempty"`
	Limit  *int   `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type ChangedFile struct {
	Path         string         `json:"path" validate:"relativePath,nonempty"`
	OldPath      string         `json:"oldPath,omitempty" validate:"relativePath"`
	Sources      []ChangeSource `json:"sources"`
	GitStatus    string         `json:"gitStatus,omitempty"`
	Turns        []int          `json:"turns,omitempty"`
	LatestPrompt string         `json:"latestPrompt,omitempty"`
	LatestTimeMs *int64         `json:"latestTime,omitempty" validate:"min=0"`
}

type WorkspaceChangesResult struct {
	Files        []ChangedFile `json:"files"`
	GitAvailable bool          `json:"gitAvailable"`
	GitBranch    string        `json:"gitBranch,omitempty"`
	HasMore      bool          `json:"hasMore"`
	NextCursor   Cursor        `json:"nextCursor,omitempty"`
}

type WorkspaceChangeDetailParams struct {
	RuntimeQuery
	Path string `json:"path" validate:"relativePath,nonempty"`
}

type WorkspaceChangeDetailResult struct {
	Diff      *string       `json:"diff,omitempty"`
	Source    *ChangeSource `json:"source,omitempty"`
	Added     int           `json:"added,omitempty" validate:"min=0"`
	Removed   int           `json:"removed,omitempty" validate:"min=0"`
	Binary    bool          `json:"binary,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
}

func (r WorkspaceChangeDetailResult) Validate() error {
	if r.Source == nil {
		if r.Diff != nil || r.Added != 0 || r.Removed != 0 || r.Binary || r.Truncated {
			return validationError("workspace change detail without source must be empty")
		}
		return nil
	}
	if r.Truncated && (r.Diff != nil || r.Added != 0 || r.Removed != 0 || r.Binary) {
		return validationError("truncated workspace change detail must omit patch fields")
	}
	return nil
}

type GitHistoryParams struct {
	RuntimeQuery
	Path string `json:"path,omitempty" validate:"relativePath"`
}

type GitCommit struct {
	Hash    string `json:"hash" validate:"gitHash"`
	Author  string `json:"author" validate:"nonempty"`
	Date    string `json:"date" validate:"rfc3339"`
	Message string `json:"message"`
}

type GitHistoryResult struct {
	Commits          []GitCommit                `json:"commits"`
	Truncated        bool                       `json:"truncated"`
	TruncationReason GitHistoryTruncationReason `json:"truncationReason,omitempty"`
	ReturnedItems    int                        `json:"returnedItems" validate:"min=0,max=100"`
}

type GitCommitDetailParams struct {
	RuntimeQuery
	Hash   string `json:"hash" validate:"gitHash"`
	Path   string `json:"path,omitempty" validate:"relativePath"`
	Cursor Cursor `json:"cursor,omitempty"`
	Limit  *int   `json:"limit,omitempty" validate:"min=1,max=1000"`
}

func (p GitCommitDetailParams) Validate() error {
	if p.Path != "" && (p.Cursor != "" || p.Limit != nil) {
		return validationError("path forbids cursor and limit")
	}
	return nil
}

type GitCommitFile struct {
	Path      string `json:"path" validate:"relativePath,nonempty"`
	OldPath   string `json:"oldPath,omitempty" validate:"relativePath"`
	Status    string `json:"status" validate:"nonempty"`
	Additions int    `json:"additions" validate:"min=0"`
	Deletions int    `json:"deletions" validate:"min=0"`
}

type GitCommitDetailResult struct {
	Kind             GitCommitDetailKind  `json:"kind"`
	Files            *[]GitCommitFile     `json:"files,omitempty"`
	HasMore          *bool                `json:"hasMore,omitempty"`
	NextCursor       Cursor               `json:"nextCursor,omitempty"`
	Path             string               `json:"path,omitempty" validate:"relativePath"`
	Body             *string              `json:"body,omitempty"`
	SizeBytes        *int64               `json:"sizeBytes,omitempty" validate:"min=0"`
	ReturnedBytes    *int64               `json:"returnedBytes,omitempty" validate:"min=0,max=1048576"`
	Truncated        *bool                `json:"truncated,omitempty"`
	TruncationReason ByteTruncationReason `json:"truncationReason,omitempty"`
}

func (r GitCommitDetailResult) Validate() error {
	switch r.Kind {
	case GitDetailFiles:
		if r.Files == nil || r.HasMore == nil {
			return validationError("files result requires files and hasMore")
		}
		if r.Path != "" || r.Body != nil || r.SizeBytes != nil || r.ReturnedBytes != nil || r.Truncated != nil || r.TruncationReason != "" {
			return validationError("files result contains patch fields")
		}
	case GitDetailPatch:
		if r.Path == "" || r.Body == nil || r.SizeBytes == nil || r.ReturnedBytes == nil || r.Truncated == nil {
			return validationError("patch result requires path, body, sizeBytes, returnedBytes, and truncated")
		}
		if r.Files != nil || r.HasMore != nil || r.NextCursor != "" {
			return validationError("patch result contains file-page fields")
		}
		if *r.ReturnedBytes > *r.SizeBytes {
			return validationError("patch returnedBytes exceeds sizeBytes")
		}
		if *r.Truncated != (*r.SizeBytes > *r.ReturnedBytes) {
			return validationError("patch truncated must match returned bytes")
		}
		if *r.Truncated != (r.TruncationReason == ByteLimit) {
			return validationError("patch truncation must use byte_limit")
		}
	}
	if r.Kind == GitDetailFiles {
		return validatePageCursor(*r.HasMore, r.NextCursor)
	}
	return nil
}
