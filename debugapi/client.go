package debugapi

// WIP interface.
// TODO: should pid/tid be exposed? For our use case, it seems it's enough to hold the current thread id internally.
//       Also ,the user will not use the thread id, unlike go routine id which is necessary to identify stack, .
type client interface {
	// LaunchProcess launches the new prcoess.
	// When returned, the process is stopped at the beginning of the program.
	LaunchProcess(name string, arg ...string) (tid int, err error)
	// AttachProcess attaches to the existing process.
	// When returned, the process is stopped.
	AttachProcess(pid int) (tid int, err error)
	DetachProcess()
	ReadMemory()
	WriteMemory()
	ReadRegisters()
	WriteRegisters()
	ContinueAndWait()
	StepAndWait()
}

// EventType represents the type of the event.
type EventType int

const (
	// EventTypeTrapped event happens when the process is trapped.
	EventTypeTrapped EventType = iota
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
