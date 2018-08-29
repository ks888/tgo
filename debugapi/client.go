package debugapi

// Client is the wrapper to transparently call the debug api which depends on the underlying OS and user's preference.
type Client interface {
	LaunchProcess()
	AttachProcess()
	ReadMemory()
	WriteMemory()
	ReadRegisters()
	WriteRegisters()
	ContinueAndWait()
	StepAndWait()
}

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
