package tracee

func (p *Process) offsetToG() int32 {
	if p.GoVersion.LaterThan(GoVersion{MajorVersion: 1, MinorVersion: 11}) {
		return 0x30
	}
	return 0x8a0
}
