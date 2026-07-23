package protocol

import (
	"time"

	"reasonix/internal/rpcwire"
)

const (
	FrameBytes                = 8 << 20
	SnapshotHistoryBytes      = 2 << 20
	ExternalizeFieldBytes     = 64 << 10
	ContentRefChunkBytes      = 256 << 10
	ContentRefObjectBytes     = 8 << 20
	ContentRefIdleMillis      = 15 * 60 * 1000
	ContentRefMaxAgeMillis    = 60 * 60 * 1000
	HistoryMaxTurns           = 200
	PageDefaultItems          = 200
	PageMaxItems              = 1000
	SearchDefaultItems        = 20
	SearchMaxItems            = 100
	SearchMaxVisitedItems     = 10000
	PreviewBytes              = 256 << 10
	GitHistoryCommits         = 100
	GitPatchBytes             = 1 << 20
	RPCConcurrentHandlers     = rpcwire.DefaultMaxConcurrentHandlers
	RPCQueuedNotifications    = 256
	LeaseTTLMillis            = 30 * 1000
	LeasePingIntervalMillis   = 10 * 1000
	IdempotencySessionEntries = 1024
	IdempotencyHostEntries    = 8192
	IdempotencyRetentionHours = 24
)

const IdempotencyRetention = 24 * time.Hour

type ProtocolLimits struct {
	FrameBytes            int `json:"frameBytes"`
	SnapshotHistoryBytes  int `json:"snapshotHistoryBytes"`
	ExternalizeFieldBytes int `json:"externalizeFieldBytes"`
	ContentRefChunkBytes  int `json:"contentRefChunkBytes"`
	ContentRefObjectBytes int `json:"contentRefObjectBytes"`
	ContentRefIdleMs      int `json:"contentRefIdleMs"`
	ContentRefMaxAgeMs    int `json:"contentRefMaxAgeMs"`
	HistoryMaxTurns       int `json:"historyMaxTurns"`
	PageDefaultItems      int `json:"pageDefaultItems"`
	PageMaxItems          int `json:"pageMaxItems"`
	SearchDefaultItems    int `json:"searchDefaultItems"`
	SearchMaxItems        int `json:"searchMaxItems"`
	SearchMaxVisitedItems int `json:"searchMaxVisitedItems"`
	PreviewBytes          int `json:"previewBytes"`
	GitHistoryCommits     int `json:"gitHistoryCommits"`
	GitPatchBytes         int `json:"gitPatchBytes"`
	RPCConcurrentHandlers int `json:"rpcConcurrentHandlers"`
}

func FrozenProtocolLimits() ProtocolLimits {
	return ProtocolLimits{
		FrameBytes: FrameBytes, SnapshotHistoryBytes: SnapshotHistoryBytes,
		ExternalizeFieldBytes: ExternalizeFieldBytes, ContentRefChunkBytes: ContentRefChunkBytes,
		ContentRefObjectBytes: ContentRefObjectBytes, ContentRefIdleMs: ContentRefIdleMillis,
		ContentRefMaxAgeMs: ContentRefMaxAgeMillis, HistoryMaxTurns: HistoryMaxTurns,
		PageDefaultItems: PageDefaultItems, PageMaxItems: PageMaxItems,
		SearchDefaultItems: SearchDefaultItems, SearchMaxItems: SearchMaxItems,
		SearchMaxVisitedItems: SearchMaxVisitedItems, PreviewBytes: PreviewBytes,
		GitHistoryCommits: GitHistoryCommits, GitPatchBytes: GitPatchBytes,
		RPCConcurrentHandlers: RPCConcurrentHandlers,
	}
}

type Features struct {
	CoreSession         bool `json:"coreSession"`
	PrimaryFileQueries  bool `json:"primaryFileQueries"`
	UserShell           bool `json:"userShell"`
	JobCancel           bool `json:"jobCancel"`
	Memory              bool `json:"memory"`
	Research            bool `json:"research"`
	MediaPreview        bool `json:"mediaPreview"`
	Attachments         bool `json:"attachments"`
	ClipboardImages     bool `json:"clipboardImages"`
	SFTP                bool `json:"sftp"`
	LocalPathOperations bool `json:"localPathOperations"`
	GitWrite            bool `json:"gitWrite"`
	PTY                 bool `json:"pty"`
	DeliveryWorktree    bool `json:"deliveryWorktree"`
}

type Capabilities struct {
	Features Features       `json:"features"`
	Limits   ProtocolLimits `json:"limits"`
}

func FrozenCapabilities(memory, research bool) Capabilities {
	return Capabilities{
		Features: Features{
			CoreSession: true, PrimaryFileQueries: true, UserShell: true, JobCancel: true,
			Memory: memory, Research: research,
		},
		Limits: FrozenProtocolLimits(),
	}
}

func (c Capabilities) Validate() error {
	f := c.Features
	if !f.CoreSession || !f.PrimaryFileQueries || !f.UserShell || !f.JobCancel {
		return validationError("coreSession, primaryFileQueries, userShell, and jobCancel must be enabled")
	}
	if f.MediaPreview || f.Attachments || f.ClipboardImages || f.SFTP || f.LocalPathOperations || f.GitWrite || f.PTY || f.DeliveryWorktree {
		return validationError("deferred Remote V1 capabilities must be disabled")
	}
	if c.Limits != FrozenProtocolLimits() {
		return validationError("capability limits do not match the frozen Remote V1 contract")
	}
	return nil
}

type LeaseLimits struct {
	TTLMillis          int `json:"ttlMs"`
	PingIntervalMillis int `json:"pingIntervalMs"`
}

type IdempotencyLimits struct {
	RetentionHours    int `json:"retentionHours"`
	PerSessionEntries int `json:"perSessionEntries"`
	PerHostEntries    int `json:"perHostEntries"`
}
