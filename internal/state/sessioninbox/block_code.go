package sessioninbox

// BlockCode is the stable identity of why an item stopped, for frontends that
// word it in their own language. BlockReason stays as the English diagnostic.
type BlockCode string

const (
	BlockSteerUnapplied   BlockCode = "steer_unapplied"
	BlockSnapshotFailed   BlockCode = "turn_snapshot_failed"
	BlockAckFailed        BlockCode = "turn_ack_failed"
	BlockOwnerInactive    BlockCode = "owner_inactive"
	BlockManifestSalvaged BlockCode = "manifest_salvaged"
	BlockForeignImages    BlockCode = "foreign_images"
)

// RetrySafe reports whether an uncertain item carrying this code never reached
// the model, so queueing it again cannot repeat a turn. Every other code, and an
// uncoded item, may already have run.
func (c BlockCode) RetrySafe() bool { return c == BlockSteerUnapplied }
