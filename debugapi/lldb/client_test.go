package lldb

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"

	"github.com/ks888/tgo/debugapi"
	"golang.org/x/sys/unix"
)

var (
	helloworldFilename        = "helloworld"
	helloworldProgram         = "testdata/" + helloworldFilename
	infloopFilename           = "infloop"
	infloopProgram            = "testdata/" + infloopFilename
	beginTextSection   uint64 = 0x1000000
	mainAddr           uint64 = 0x1051430
)

// The debugserver exits when the connection is closed.
// So do not need to kill the process here.

func TestLaunchProcess(t *testing.T) {
	client := NewClient()
	tid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if tid == 0 {
		t.Errorf("invalid tid: %d", tid)
	}
}

func TestLaunchProcess_ProgramNotExist(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess("notexist")
	if err == nil {
		t.Fatalf("error not returned")
	}
}

func TestAttachProcess(t *testing.T) {
	cmd := exec.Command(infloopProgram)
	_ = cmd.Start()

	client := NewClient()
	tid, err := client.AttachProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if tid == 0 {
		t.Errorf("invalid tid: %d", tid)
	}
}

func TestAttachProcess_WrongPID(t *testing.T) {
	client := NewClient()
	cmd := exec.Command(helloworldProgram)
	_ = cmd.Run()

	// the program already exits, so the pid is wrong
	_, err := client.AttachProcess(cmd.Process.Pid)
	if err == nil {
		t.Fatalf("error should be returned")
	}
}

func TestDetachProcess(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = client.DetachProcess()
	if err != nil {
		t.Fatalf("failed to detach from the process: %v", err)
	}
}

func TestReadRegisters(t *testing.T) {
	client := NewClient()
	tid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	_ = client.WriteMemory(mainAddr, []byte{0xcc})
	_, _, _ = client.ContinueAndWait()

	regs, err := client.ReadRegisters(tid)
	if err != nil {
		t.Fatalf("failed to read registers: %v", err)
	}
	if regs.Rip != uint64(mainAddr+1) {
		t.Fatalf("wrong rip: %x", regs.Rip)
	}
	if regs.Rsp == 0 {
		t.Fatalf("empty rsp: %x", regs.Rsp)
	}
}

func TestWriteRegisters(t *testing.T) {
	client := NewClient()
	tid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	regs := debugapi.Registers{Rip: 0x1, Rsp: 0x2, Rcx: 0x3}
	if err := client.WriteRegisters(tid, regs); err != nil {
		t.Fatalf("failed to write registers: %v", err)
	}

	actualRegs, _ := client.ReadRegisters(tid)
	if actualRegs.Rip != 0x1 {
		t.Errorf("wrong rip: %x", actualRegs.Rip)
	}
	if actualRegs.Rsp != 0x2 {
		t.Errorf("wrong rsp: %x", actualRegs.Rsp)
	}
	if actualRegs.Rcx != 0x3 {
		t.Errorf("wrong rcx: %x", actualRegs.Rcx)
	}
}

func TestAllocateMemory(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	addr, err := client.allocateMemory(1)
	if err != nil {
		t.Fatalf("failed to allocate memory: %v", err)
	}

	if addr == 0 {
		t.Errorf("empty addr: %x", addr)
	}
}

func TestDeallocateMemory(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	addr, _ := client.allocateMemory(1)
	err = client.deallocateMemory(addr)
	if err != nil {
		t.Fatalf("failed to deallocate memory: %v", err)
	}
}

func TestReadMemory(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	out := make([]byte, 2)
	err = client.ReadMemory(beginTextSection, out)
	if err != nil {
		t.Fatalf("failed to read memory: %v", err)
	}

	if out[0] != 0xcf || out[1] != 0xfa {
		t.Errorf("wrong memory: %v", out)
	}
}

func TestWriteMemory(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	data := []byte{0x1, 0x2, 0x3, 0x4}
	err = client.WriteMemory(beginTextSection, data)
	if err != nil {
		t.Fatalf("failed to write memory: %v", err)
	}

	actual := make([]byte, 4)
	_ = client.ReadMemory(beginTextSection, actual)
	if actual[0] != 0x1 || actual[1] != 0x2 || actual[2] != 0x3 || actual[3] != 0x4 {
		t.Errorf("wrong memory: %v", actual)
	}

}

func TestReadTLS(t *testing.T) {
	client := NewClient()
	tid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	_ = client.WriteMemory(mainAddr, []byte{0xcc})
	_, _, _ = client.ContinueAndWait()

	var offset uint32 = 0xf
	_, err = client.ReadTLS(tid, offset)
	if err != nil {
		t.Fatalf("failed to read tls: %v", err)
	}
	if client.currentOffset != offset {
		t.Errorf("wrong offset: %x", client.currentOffset)
	}

	// depends on os and cpu
	// buff := make([]byte, 8)
	// var offsetToID uint64 = 8*2 + 8 + 8 + 8 + 8 + 8 + 8*7 + 8 + 8 + 8 + 8 + 4 + 4
	// _ = client.ReadMemory(gAddr+offsetToID, buff)
	// goRoutineID := binary.LittleEndian.Uint64(buff)
	// if goRoutineID != 1 {
	// 	t.Errorf("unexpected go routine id: %d", goRoutineID)
	// }
}

func TestContinueAndWait_Trapped(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	out := []byte{0xcc}
	err = client.WriteMemory(mainAddr, out)
	if err != nil {
		t.Fatalf("failed to write memory: %v", err)
	}

	tid, event, err := client.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if tid == 0 {
		t.Errorf("empty tid")
	}
	if event != (debugapi.Event{Type: debugapi.EventTypeTrapped}) {
		t.Errorf("wrong event: %v", event)
	}
}

func TestContinueAndWait_Exited(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(helloworldProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	for {
		_, event, err := client.ContinueAndWait()
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}
		if event == (debugapi.Event{Type: debugapi.EventTypeExited}) {
			break
		}
	}
}

func TestContinueAndWait_ConsoleWrite(t *testing.T) {
	client := NewClient()
	buff := &bytes.Buffer{}
	client.outputWriter = buff
	_, err := client.LaunchProcess(helloworldProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	for {
		_, _, err := client.ContinueAndWait()
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}
		if buff.String() == "Hello, playground\r\n" {
			break
		}
	}
}

func TestContinueAndWait_Signaled(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	pid, _ := findProcessID(infloopFilename, client.pid)
	// Note that the debugserver does not pass the signals like SIGTERM and SIGINT to the debugee.
	_ = sendSignal(pid, unix.SIGKILL)

	_, event, err := client.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if event != (debugapi.Event{Type: debugapi.EventTypeTerminated}) {
		t.Fatalf("wrong event: %v", event)
	}
}

func TestContinueAndWait_Stopped(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(helloworldProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	pid, _ := findProcessID(helloworldFilename, client.pid)
	_ = sendSignal(pid, unix.SIGUSR1)

	// non-SIGTRAP signal is handled internally.
	_, event, err := client.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if event != (debugapi.Event{Type: debugapi.EventTypeExited}) {
		t.Fatalf("wrong event: %v", event)
	}
}

// No test for CoreDump as the debugserver does not pass the signals like SIGQUIT to the debugee.

func TestStepAndWait(t *testing.T) {
	client := NewClient()
	tid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	newTid, event, err := client.StepAndWait(tid)
	if err != nil {
		t.Fatalf("failed to step and wait: %v", err)
	}
	if event != (debugapi.Event{Type: debugapi.EventTypeTrapped}) {
		t.Fatalf("wrong event: %v", event)
	}
	if tid != newTid {
		t.Fatalf("wrong tid: %d != %d", tid, newTid)
	}
}

func TestStepAndWait_StopAtBreakpoint(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	orgInsts := make([]byte, 1)
	_ = client.ReadMemory(mainAddr, orgInsts)
	_ = client.WriteMemory(mainAddr, []byte{0xcc})
	tid, _, _ := client.ContinueAndWait()

	regs, _ := client.ReadRegisters(tid)
	regs.Rip--
	_ = client.WriteRegisters(tid, regs)
	_ = client.WriteMemory(mainAddr, orgInsts)

	_, _, err = client.StepAndWait(tid)
	if err != nil {
		t.Fatalf("failed to step and wait: %v", err)
	}

	regs, _ = client.ReadRegisters(tid)
	if regs.Rip != uint64(mainAddr)+9 {
		t.Errorf("wrong pc: %x", regs.Rip)
	}
}

func findProcessID(progName string, parentPID int) (int, error) {
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(parentPID), progName).Output()
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(string(out[0 : len(out)-1])) // remove newline
}

func sendSignal(pid int, signal syscall.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	return proc.Signal(signal)
}

func TestSetNoAckMode(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, false)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "QStartNoAckMode" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("OK"); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, false)

	if err := client.setNoAckMode(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !client.noAckMode {
		t.Errorf("ack mode is not set")
	}

	<-sendDone
}

func TestSetNoAckMode_ErrorReturned(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, false)
		_, _ = client.receive()
		_ = client.send("E00")
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, false)

	if err := client.setNoAckMode(); err == nil {
		t.Errorf("error is not returned")
	}

	<-sendDone
}

func TestQSupported(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "qSupported:swbreak+;hwbreak+;no-resumed+" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("qXfer:features:read+;PacketSize=20000;qEcho+"); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	if err := client.qSupported(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	<-sendDone
}

func TestCollectRegisterMetadata(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		_, _ = client.receive()
		_ = client.send("name:rax;bitsize:64;offset:0;")
		_, _ = client.receive()
		_ = client.send("name:rbx;bitsize:64;offset:8;")
		_, _ = client.receive()
		_ = client.send("E45")

	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	meatadata, err := client.collectRegisterMetadata()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meatadata) != 2 {
		t.Errorf("wrong length of register metadata: %d", len(meatadata))
	}

	<-sendDone
}

func TestQRegisterInfo(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "qRegisterInfo0" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("name:rax;bitsize:64;offset:0;encoding:uint;format:hex;set:General Purpose Registers;ehframe:0;dwarf:0;invalidate-regs:0,15,25,35,39;"); err != nil {
			t.Fatalf("failed to send response: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	reg, err := client.qRegisterInfo(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.name != "rax" {
		t.Errorf("wrong name: %s", reg.name)
	}
	if reg.offset != 0 {
		t.Errorf("wrong offset: %d", reg.offset)
	}
	if reg.size != 8 {
		t.Errorf("wrong size: %d", reg.size)
	}

	<-sendDone
}

func TestQRegisterInfo_EndOfRegisterList(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		_, _ = client.receive()
		_ = client.send("E45")
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	_, err := client.qRegisterInfo(0)
	if err != errEndOfList {
		t.Fatalf("unexpected error: %v", err)
	}

	<-sendDone
}

func TestQListThreadsInStopReply(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "QListThreadsInStopReply" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("OK"); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	if err := client.qListThreadsInStopReply(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	<-sendDone
}

func TestQfThreadInfo(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "qfThreadInfo" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("m15296fb"); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	tid, err := client.qfThreadInfo()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tid != "15296fb" {
		t.Errorf("unexpected tid: %v", tid)
	}

	<-sendDone
}

func TestSendAndReceive(t *testing.T) {
	connForReceive, connForSend := net.Pipe()
	cmd := "command"

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, false)
		if err := client.send(cmd); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, false)
	buff, err := client.receive()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != buff {
		t.Errorf("receieved unexpected data: %v", buff)
	}

	<-sendDone
}

func TestSendAndReceive_NoAckMode(t *testing.T) {
	connForReceive, connForSend := net.Pipe()
	cmd := "command"

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if err := client.send(cmd); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)
	buff, err := client.receive()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != buff {
		t.Errorf("receieved unexpected data: %v", buff)
	}

	<-sendDone
}

func TestVerifyPacket(t *testing.T) {
	for i, test := range []struct {
		packet      string
		expectError bool
	}{
		{packet: "$command#df", expectError: false},
		{packet: "#command#df", expectError: true},
		{packet: "$command$df", expectError: true},
		{packet: "$command#00", expectError: true},
	} {
		actual := verifyPacket(test.packet)
		if test.expectError && actual == nil {
			t.Errorf("[%d] error not returned", i)
		} else if !test.expectError && actual != nil {
			t.Errorf("[%d] error returned: %v", i, actual)
		}
	}
}

func TestHexToUint64(t *testing.T) {
	for i, test := range []struct {
		hex          string
		littleEndian bool
		expected     uint64
	}{
		{hex: "00000001", littleEndian: false, expected: 1},
		{hex: "00000102", littleEndian: false, expected: 258},
		{hex: "02010000", littleEndian: true, expected: 258},
	} {
		actual, _ := hexToUint64(test.hex, test.littleEndian)
		if test.expected != actual {
			t.Errorf("[%d] not expected value: %d", i, actual)
		}
	}
}

func TestUint64ToHex(t *testing.T) {
	for i, test := range []struct {
		input        uint64
		littleEndian bool
		expected     string
	}{
		{input: 1, littleEndian: false, expected: "0000000000000001"},
		{input: 258, littleEndian: false, expected: "0000000000000102"},
		{input: 258, littleEndian: true, expected: "0201000000000000"},
	} {
		actual := uint64ToHex(test.input, test.littleEndian)
		if test.expected != actual {
			t.Errorf("[%d] not expected value: %s", i, actual)
		}
	}
}

func TestChecksum(t *testing.T) {
	for i, data := range []struct {
		input    []byte
		expected uint8
	}{
		{input: []byte{0x1, 0x2}, expected: 3},
		{input: []byte{0x7f, 0x80}, expected: 255},
		{input: []byte{0x80, 0x80}, expected: 0},
		{input: []byte{0x63, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64}, expected: 0xdf},
	} {
		sum := calcChecksum(data.input)
		if sum != data.expected {
			t.Errorf("[%d] wrong checksum: %x", i, sum)
		}
	}
}

func newTestClient(conn net.Conn, noAckMode bool) *Client {
	return &Client{conn: conn, noAckMode: noAckMode, buffer: make([]byte, maxPacketSize)}
}
