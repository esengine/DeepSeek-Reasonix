package runtimeapi

import "fmt"

type FileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

type FileListInput struct {
	Session SessionRef `json:"session"`
	Path    string     `json:"path"`
	Cursor  Cursor     `json:"cursor,omitempty"`
	Limit   int        `json:"limit,omitempty"`
}

type FileListResult struct {
	Entries []FileEntry `json:"entries"`
	HasMore bool        `json:"hasMore"`
	Next    Cursor      `json:"next,omitempty"`
}

type FileSearchInput struct {
	Session SessionRef `json:"session"`
	Query   string     `json:"query"`
	Limit   int        `json:"limit,omitempty"`
}

type SearchTruncationReason string

const (
	SearchResultLimit SearchTruncationReason = "result_limit"
	SearchScanLimit   SearchTruncationReason = "scan_limit"
)

type FileSearchResult struct {
	Entries          []FileEntry            `json:"entries"`
	Truncated        bool                   `json:"truncated"`
	TruncationReason SearchTruncationReason `json:"truncationReason,omitempty"`
	ReturnedItems    int                    `json:"returnedItems"`
	TotalItems       *int                   `json:"totalItems,omitempty"`
}

type FilePreviewInput struct {
	Session SessionRef `json:"session"`
	Path    string     `json:"path"`
}

type FileKind string

const (
	FileText   FileKind = "text"
	FileBinary FileKind = "binary"
	FileImage  FileKind = "image"
	FilePDF    FileKind = "pdf"
)

type ByteTruncationReason string

const ByteLimit ByteTruncationReason = "byte_limit"

type FilePreview struct {
	Name             string               `json:"name"`
	Path             string               `json:"path"`
	Kind             FileKind             `json:"kind"`
	SizeBytes        int64                `json:"sizeBytes"`
	ReturnedBytes    int64                `json:"returnedBytes"`
	Binary           bool                 `json:"binary"`
	Truncated        bool                 `json:"truncated"`
	TruncationReason ByteTruncationReason `json:"truncationReason,omitempty"`
	Body             *string              `json:"body,omitempty"`
}

func (r FilePreview) Validate() error {
	if r.SizeBytes < 0 || r.ReturnedBytes < 0 || r.ReturnedBytes > PreviewBytes {
		return fmt.Errorf("runtimeapi: invalid file preview byte counts")
	}
	if r.Kind == FileText {
		if r.Binary || r.Body == nil || r.ReturnedBytes > r.SizeBytes {
			return fmt.Errorf("runtimeapi: text preview has inconsistent binary or byte fields")
		}
		if r.Truncated != (r.SizeBytes > r.ReturnedBytes) || r.Truncated != (r.TruncationReason == ByteLimit) {
			return fmt.Errorf("runtimeapi: text preview has inconsistent truncation fields")
		}
		return nil
	}
	if !r.Binary || r.ReturnedBytes != 0 || r.Body != nil || r.Truncated || r.TruncationReason != "" {
		return fmt.Errorf("runtimeapi: binary, image, and pdf previews are metadata only")
	}
	return nil
}

type WorkspaceChangesInput struct {
	Session SessionRef `json:"session"`
	Cursor  Cursor     `json:"cursor,omitempty"`
	Limit   int        `json:"limit,omitempty"`
}

type ChangeSource string

const (
	ChangeSession ChangeSource = "session"
	ChangeGit     ChangeSource = "git"
)

type ChangedFile struct {
	Path             string         `json:"path"`
	OldPath          string         `json:"oldPath,omitempty"`
	Sources          []ChangeSource `json:"sources"`
	GitStatus        string         `json:"gitStatus,omitempty"`
	Turns            []int          `json:"turns,omitempty"`
	LatestPrompt     string         `json:"latestPrompt,omitempty"`
	LatestTimeMillis *int64         `json:"latestTimeMillis,omitempty"`
}

type WorkspaceChangesPage struct {
	Files        []ChangedFile `json:"files"`
	GitAvailable bool          `json:"gitAvailable"`
	GitBranch    string        `json:"gitBranch,omitempty"`
	HasMore      bool          `json:"hasMore"`
	Next         Cursor        `json:"next,omitempty"`
}

type GitHistoryInput struct {
	Session SessionRef `json:"session"`
	Path    string     `json:"path,omitempty"`
}

type GitCommit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

type GitHistoryTruncationReason string

const GitHistoryLimit GitHistoryTruncationReason = "history_limit"

type GitHistoryResult struct {
	Commits          []GitCommit                `json:"commits"`
	Truncated        bool                       `json:"truncated"`
	TruncationReason GitHistoryTruncationReason `json:"truncationReason,omitempty"`
	ReturnedItems    int                        `json:"returnedItems"`
}

type GitCommitDetailInput struct {
	Session SessionRef `json:"session"`
	Hash    string     `json:"hash"`
	Path    string     `json:"path,omitempty"`
	Cursor  Cursor     `json:"cursor,omitempty"`
	Limit   int        `json:"limit,omitempty"`
}

func (p GitCommitDetailInput) Validate() error {
	if p.Path != "" && (p.Cursor != "" || p.Limit != 0) {
		return fmt.Errorf("runtimeapi: Git commit path forbids cursor and limit")
	}
	return ValidatePageLimit(p.Limit)
}

type GitCommitDetailKind string

const (
	GitDetailFiles GitCommitDetailKind = "files"
	GitDetailPatch GitCommitDetailKind = "patch"
)

type GitCommitFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type GitCommitDetail struct {
	Kind             GitCommitDetailKind  `json:"kind"`
	Files            *[]GitCommitFile     `json:"files,omitempty"`
	HasMore          *bool                `json:"hasMore,omitempty"`
	Next             Cursor               `json:"next,omitempty"`
	Path             string               `json:"path,omitempty"`
	Body             *string              `json:"body,omitempty"`
	SizeBytes        *int64               `json:"sizeBytes,omitempty"`
	ReturnedBytes    *int64               `json:"returnedBytes,omitempty"`
	Truncated        *bool                `json:"truncated,omitempty"`
	TruncationReason ByteTruncationReason `json:"truncationReason,omitempty"`
}

func (r GitCommitDetail) Validate() error {
	switch r.Kind {
	case GitDetailFiles:
		if r.Files == nil || r.HasMore == nil {
			return fmt.Errorf("runtimeapi: files result requires files and hasMore")
		}
		if r.Path != "" || r.Body != nil || r.SizeBytes != nil || r.ReturnedBytes != nil || r.Truncated != nil || r.TruncationReason != "" {
			return fmt.Errorf("runtimeapi: files result contains patch fields")
		}
		if *r.HasMore != (r.Next != "") {
			return fmt.Errorf("runtimeapi: files result has inconsistent cursor")
		}
		return nil
	case GitDetailPatch:
		if r.Path == "" || r.Body == nil || r.SizeBytes == nil || r.ReturnedBytes == nil || r.Truncated == nil {
			return fmt.Errorf("runtimeapi: patch result requires path, body, sizeBytes, returnedBytes, and truncated")
		}
		if r.Files != nil || r.HasMore != nil || r.Next != "" {
			return fmt.Errorf("runtimeapi: patch result contains file-page fields")
		}
		if *r.SizeBytes < 0 || *r.ReturnedBytes < 0 || *r.ReturnedBytes > *r.SizeBytes || *r.ReturnedBytes > GitPatchBytes {
			return fmt.Errorf("runtimeapi: patch result has invalid byte counts")
		}
		if *r.Truncated != (*r.SizeBytes > *r.ReturnedBytes) || *r.Truncated != (r.TruncationReason == ByteLimit) {
			return fmt.Errorf("runtimeapi: patch result has inconsistent truncation fields")
		}
		return nil
	default:
		return fmt.Errorf("runtimeapi: unknown Git commit detail kind %q", r.Kind)
	}
}
