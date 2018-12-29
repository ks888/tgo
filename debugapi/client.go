package debugapi

import (
	"fmt"
)

// client is the client interface to control the tracee process.
// It's still unstable and so do not export it.
type client interface {
	// LaunchProcess launches the new prcoess.
	LaunchProcess(name string, arg ...string) error
	// AttachProcess attaches to the existing process.
	AttachProcess(pid int) error
	DetachProcess() error
	ReadMemory(addr uint64, out []byte) error
	WriteMemory(addr uint64, data []byte) error
	ReadRegisters(threadID int) (Registers, error)
	WriteRegisters(threadID int, regs Registers) error
	ReadTLS(offset uint64) (value uint64)
	ContinueAndWait() (Event, error)
	StepAndWait(threadID int) (Event, error)
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

// IsExitEvent returns true if the event indicates the process exits for some reason.
func IsExitEvent(event EventType) bool {
	return event == EventTypeCoreDump || event == EventTypeExited || event == EventTypeTerminated
}

// Event describes the event happens to the target process.
type Event struct {
	Type EventType
	// Data is one of these go types:
	//
	//    EventType            Go type     Description
	//    -----------          -------     -----------
	//    EventTypeTrapped     []int       A list of trapped thread id
	//    EventTypeCoreDump    NA          NA
	//    EventTypeExited      int         Exit status
	//    EventTypeTerminated  int         Signal number
	Data interface{}
}

// Registers represents the target's registers.
type Registers struct {
	Rip uint64
	Rsp uint64
	Rcx uint64
}

// UnspecifiedThreadError indicates the stopped threads include unspecified ones.
type UnspecifiedThreadError struct {
	ThreadIDs []int
}

// Error returns the list of unspecified threads.
func (e UnspecifiedThreadError) Error() string {
	return fmt.Sprintf("unspecified threads: %v", e.ThreadIDs)
}
