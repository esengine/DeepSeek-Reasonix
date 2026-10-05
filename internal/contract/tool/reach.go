package tool

// Reach is how far one call of a tool can act beyond the conversation. A tool
// states it about itself; the host never reads it off a name. The zero value
// says nothing, which is why a tool nobody classified is never admitted where
// reach matters.
type Reach uint8

const (
	// ReachUnstated is the zero value: nothing was declared.
	ReachUnstated Reach = iota
	// ReachLocalRead only reads files through the host-confined readers: it
	// starts no program of the model's choosing, touches no network and leaves
	// nothing behind.
	ReachLocalRead
	// ReachHostControl only hands the turn back to the host: it records a
	// conclusion or a question and changes nothing outside the session.
	ReachHostControl
)

// ReachDeclarer is implemented by a tool that states its Reach.
type ReachDeclarer interface {
	Reach() Reach
}

// ReachOf returns what t declared, or ReachUnstated.
func ReachOf(t Tool) Reach {
	if d, ok := t.(ReachDeclarer); ok {
		return d.Reach()
	}
	return ReachUnstated
}
