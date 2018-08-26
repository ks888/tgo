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
