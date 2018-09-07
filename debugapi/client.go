package debugapi

// EventType represents the type of the event.
type EventType int

// TODO: should integrate CoreDump, Exited, Terminated?
const (
	// EventTypeCreated event happens when the new thread is created
	EventTypeCreated EventType = iota
	// EventTypeTrapped event happens when the process is trapped.
	EventTypeTrapped
	// EventTypeCoreDump event happens when the process terminates unexpectedly.
	EventTypeCoreDump
	// EventTypeExited event happens when the process exits.
	EventTypeExited
	// EventTypeTerminated event happens when the process is terminated by a signal.
	EventTypeTerminated
)

// Event describes the event happens to the target process.
type Event struct {
	Type EventType
	Data int
}

// Registers represents the target's registers.
type Registers struct {
	Rip uint64
	Rsp uint64
}
