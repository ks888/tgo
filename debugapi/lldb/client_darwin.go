package lldb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/log"
	"golang.org/x/sys/unix"
)

const debugServerPath = "/Library/Developer/CommandLineTools/Library/PrivateFrameworks/LLDB.framework/Versions/A/Resources/debugserver"

// Assumes the packet size is not larger than this.
const maxPacketSize = 4096

const excBadAccess = syscall.Signal(0x91) // EXC_BAD_ACCESS

// Client is the debug api client which depends on lldb's debugserver.
// See the gdb's doc for the reference: https://sourceware.org/gdb/onlinedocs/gdb/Remote-Protocol.html
// Some commands use the lldb extension: https://github.com/llvm-mirror/lldb/blob/master/docs/lldb-gdb-remote.txt
type Client struct {
	conn                 net.Conn
	pid                  int
	killOnDetach         bool
	noAckMode            bool
	registerMetadataList []registerMetadata
	buffer               []byte
	// outputWriter is the writer to which the output of the debugee process will be written.
	outputWriter io.Writer

	readTLSFuncAddr  uint64
	currentTLSOffset uint32
}

// NewClient returns the new debug api client which depends on OS API.
func NewClient() *Client {
	return &Client{buffer: make([]byte, maxPacketSize), outputWriter: os.Stdout}
}

// LaunchProcess lets the debugserver launch the new prcoess.
func (c *Client) LaunchProcess(name string, arg ...string) error {
	listener, err := net.Listen("tcp", "localhost:")
	if err != nil {
		return err
	}

	debugServerArgs := []string{"-F", "-R", listener.Addr().String(), "--", name}
	debugServerArgs = append(debugServerArgs, arg...)
	cmd := exec.Command(debugServerPath, debugServerArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Otherwise, the signal sent to all the group members.
	if err := cmd.Start(); err != nil {
		return err
	}

	c.conn, err = c.waitConnectOrExit(listener, cmd)
	if err != nil {
		return err
	}
	c.pid = cmd.Process.Pid
	c.killOnDetach = true

	return c.initialize()
}

func (c *Client) waitConnectOrExit(listener net.Listener, cmd *exec.Cmd) (net.Conn, error) {
	waitCh := make(chan error)
	go func(ch chan error) {
		ch <- cmd.Wait()
	}(waitCh)

	connCh := make(chan net.Conn)
	go func(ch chan net.Conn) {
		conn, err := listener.Accept()
		if err != nil {
			connCh <- nil
		}
		connCh <- conn
	}(connCh)

	select {
	case <-waitCh:
		return nil, errors.New("the command exits immediately")
	case conn := <-connCh:
		if conn == nil {
			return nil, errors.New("failed to accept the connection")
		}
		return conn, nil
	}
}

func (c *Client) initialize() error {
	if err := c.setNoAckMode(); err != nil {
		return err
	}

	if err := c.qSupported(); err != nil {
		return err
	}

	if err := c.qThreadSuffixSupported(); err != nil {
		return err
	}

	var err error
	c.registerMetadataList, err = c.collectRegisterMetadata()
	if err != nil {
		return err
	}

	if err := c.qListThreadsInStopReply(); err != nil {
		return err
	}

	readTLSFunction := c.buildReadTLSFunction(0) // need the function length here. So the offset doesn't matter.
	c.readTLSFuncAddr, err = c.allocateMemory(len(readTLSFunction))
	return err
}

func (c *Client) setNoAckMode() error {
	const command = "QStartNoAckMode"
	if err := c.send(command); err != nil {
		return err
	}

	if err := c.receiveAndCheck(); err != nil {
		return err
	}

	c.noAckMode = true
	return nil
}

func (c *Client) qSupported() error {
	var supportedFeatures = []string{"swbreak+", "hwbreak+", "no-resumed+"}
	command := fmt.Sprintf("qSupported:%s", strings.Join(supportedFeatures, ";"))
	if err := c.send(command); err != nil {
		return err
	}

	// TODO: adjust the buffer size so that it doesn't exceed the PacketSize in the response.
	_, err := c.receive()
	return err
}

func (c *Client) qThreadSuffixSupported() error {
	const command = "QThreadSuffixSupported"
	if err := c.send(command); err != nil {
		return err
	}
	return c.receiveAndCheck()
}

var errEndOfList = errors.New("the end of list")

type registerMetadata struct {
	name             string
	id, offset, size int
}

func (c *Client) collectRegisterMetadata() ([]registerMetadata, error) {
	var regs []registerMetadata
	for i := 0; ; i++ {
		reg, err := c.qRegisterInfo(i)
		if err != nil {
			if err == errEndOfList {
				break
			}
			return nil, err
		}
		regs = append(regs, reg)
	}

	return regs, nil
}

func (c *Client) qRegisterInfo(registerID int) (registerMetadata, error) {
	command := fmt.Sprintf("qRegisterInfo%x", registerID)
	if err := c.send(command); err != nil {
		return registerMetadata{}, err
	}

	data, err := c.receive()
	if err != nil {
		return registerMetadata{}, err
	}

	if strings.HasPrefix(data, "E") {
		if data == "E45" {
			return registerMetadata{}, errEndOfList
		}
		return registerMetadata{}, fmt.Errorf("error response: %s", data)
	}

	return c.parseRegisterMetaData(registerID, data)
}

func (c *Client) parseRegisterMetaData(registerID int, data string) (registerMetadata, error) {
	reg := registerMetadata{id: registerID}
	for _, chunk := range strings.Split(data, ";") {
		keyValue := strings.SplitN(chunk, ":", 2)
		if len(keyValue) < 2 {
			continue
		}

		key, value := keyValue[0], keyValue[1]
		if key == "name" {
			reg.name = value

		} else if key == "bitsize" {
			num, err := strconv.Atoi(value)
			if err != nil {
				return registerMetadata{}, err
			}
			reg.size = num / 8

		} else if key == "offset" {
			num, err := strconv.Atoi(value)
			if err != nil {
				return registerMetadata{}, err
			}

			reg.offset = num
		}
	}

	return reg, nil
}

func (c *Client) qListThreadsInStopReply() error {
	const command = "QListThreadsInStopReply"
	if err := c.send(command); err != nil {
		return err
	}

	return c.receiveAndCheck()
}

func (c *Client) allocateMemory(size int) (uint64, error) {
	command := fmt.Sprintf("_M%x,rwx", size)
	if err := c.send(command); err != nil {
		return 0, err
	}

	data, err := c.receive()
	if err != nil {
		return 0, err
	} else if data == "" || strings.HasPrefix(data, "E") {
		return 0, fmt.Errorf("error response: %s", data)
	}

	return hexToUint64(data, false)
}

func (c *Client) deallocateMemory(addr uint64) error {
	command := fmt.Sprintf("_m%x", addr)
	if err := c.send(command); err != nil {
		return err
	}

	return c.receiveAndCheck()
}

// ThreadIDs returns all the thread ids.
func (c *Client) ThreadIDs() ([]int, error) {
	rawThreadIDs, err := c.qfThreadInfo()
	if err != nil {
		return nil, err
	}
	// TODO: call qsThreadInfo

	var threadIDs []int
	for _, rawThreadID := range strings.Split(rawThreadIDs, ",") {
		tid, err := hexToUint64(rawThreadID, false)
		if err != nil {
			return nil, err
		}
		threadIDs = append(threadIDs, int(tid))
	}
	return threadIDs, nil
}

func (c *Client) qfThreadInfo() (string, error) {
	const command = "qfThreadInfo"
	if err := c.send(command); err != nil {
		return "", err
	}

	data, err := c.receive()
	if err != nil {
		return "", err
	} else if !strings.HasPrefix(data, "m") {
		return "", fmt.Errorf("unexpected response: %s", data)
	}

	return data[1:], nil
}

// AttachProcess lets the debugserver attach the new prcoess.
func (c *Client) AttachProcess(pid int) error {
	listener, err := net.Listen("tcp", "localhost:")
	if err != nil {
		return err
	}

	debugServerArgs := []string{"-F", "-R", listener.Addr().String(), fmt.Sprintf("--attach=%d", pid)}
	cmd := exec.Command(debugServerPath, debugServerArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Otherwise, the signal sent to all the group members.
	if err := cmd.Start(); err != nil {
		return err
	}

	c.conn, err = c.waitConnectOrExit(listener, cmd)
	if err != nil {
		return err
	}
	c.pid = cmd.Process.Pid

	return c.initialize()
}

// DetachProcess detaches from the prcoess.
func (c *Client) DetachProcess() error {
	defer c.close()
	if c.killOnDetach {
		return c.killProcess()
	}

	if err := c.send("D"); err != nil {
		return err
	}

	return c.receiveAndCheck()
}

func (c *Client) close() error {
	return c.conn.Close()
}

func (c *Client) killProcess() error {
	if err := c.send("k"); err != nil {
		return err
	}
	data, err := c.receive()
	if err != nil {
		return err
	} else if !strings.HasPrefix(data, "X09") {
		return fmt.Errorf("unexpected reply: %s", data)
	}
	// debugserver automatically exits. So don't explicitly detach here.
	return nil
}

// ReadRegisters reads the target tid's registers.
func (c *Client) ReadRegisters(tid int) (debugapi.Registers, error) {
	data, err := c.readRegisters(tid)
	if err != nil {
		return debugapi.Registers{}, err
	}

	return c.parseRegisterData(data)
}

func (c *Client) readRegisters(tid int) (string, error) {
	command := fmt.Sprintf("g;thread:%x;", tid)
	if err := c.send(command); err != nil {
		return "", err
	}

	data, err := c.receive()
	if err != nil {
		return "", err
	} else if strings.HasPrefix(data, "E") {
		return data, fmt.Errorf("error response: %s", data)
	}
	return data, nil
}

func (c *Client) parseRegisterData(data string) (debugapi.Registers, error) {
	var regs debugapi.Registers
	for _, metadata := range c.registerMetadataList {
		rawValue := data[metadata.offset*2 : (metadata.offset+metadata.size)*2]

		var err error
		switch metadata.name {
		case "rip":
			regs.Rip, err = hexToUint64(rawValue, true)
		case "rsp":
			regs.Rsp, err = hexToUint64(rawValue, true)
		case "rcx":
			regs.Rcx, err = hexToUint64(rawValue, true)
		}
		if err != nil {
			return debugapi.Registers{}, err
		}
	}

	return regs, nil
}

// WriteRegisters updates the registers' value.
func (c *Client) WriteRegisters(tid int, regs debugapi.Registers) error {
	data, err := c.readRegisters(tid)
	if err != nil {
		return err
	}

	// The 'P' command is not used due to the bug explained here: https://github.com/llvm-mirror/lldb/commit/d8d7a40ca5377aa777e3840f3e9b6a63c6b09445

	for _, metadata := range c.registerMetadataList {
		prefix := data[0 : metadata.offset*2]
		suffix := data[(metadata.offset+metadata.size)*2:]

		var err error
		switch metadata.name {
		case "rip":
			data = fmt.Sprintf("%s%s%s", prefix, uint64ToHex(regs.Rip, true), suffix)
		case "rsp":
			data = fmt.Sprintf("%s%s%s", prefix, uint64ToHex(regs.Rsp, true), suffix)
		case "rcx":
			data = fmt.Sprintf("%s%s%s", prefix, uint64ToHex(regs.Rcx, true), suffix)
		}
		if err != nil {
			return err
		}
	}

	command := fmt.Sprintf("G%s;thread:%x;", data, tid)
	if err := c.send(command); err != nil {
		return err
	}

	return c.receiveAndCheck()
}

// ReadMemory reads the specified memory region.
func (c *Client) ReadMemory(addr uint64, out []byte) error {
	if len(out) > maxPacketSize-4 /* header 1 byte + footer 3 bytes */ {
		// TODO: Remove the max size constraint.
		return fmt.Errorf("can't read the memory region larger than %d (specified %d)", maxPacketSize-4, len(out))
	}

	command := fmt.Sprintf("m%x,%x", addr, len(out))
	if err := c.send(command); err != nil {
		return err
	}

	data, err := c.receive()
	if err != nil {
		return err
	} else if strings.HasPrefix(data, "E") {
		return fmt.Errorf("error response: %s", data)
	}

	byteArrary, err := hexToByteArray(data)
	if err != nil {
		return err
	}
	copy(out, byteArrary)
	return nil
}

// WriteMemory write the data to the specified region
func (c *Client) WriteMemory(addr uint64, data []byte) error {
	dataInHex := ""
	for _, b := range data {
		dataInHex += fmt.Sprintf("%02x", b)
	}
	command := fmt.Sprintf("M%x,%x:%s", addr, len(data), dataInHex)
	if err := c.send(command); err != nil {
		return err
	}

	return c.receiveAndCheck()
}

// ReadTLS reads the offset from the beginning of the TLS block.
func (c *Client) ReadTLS(tid int, offset uint32) (uint64, error) {
	if err := c.updateReadTLSFunction(offset); err != nil {
		return 0, err
	}

	originalRegs, err := c.ReadRegisters(tid)
	if err != nil {
		return 0, err
	}
	defer func() { err = c.WriteRegisters(tid, originalRegs) }()

	modifiedRegs := originalRegs
	modifiedRegs.Rip = c.readTLSFuncAddr
	if err = c.WriteRegisters(tid, modifiedRegs); err != nil {
		return 0, err
	}

	if _, err := c.StepAndWait(tid); err != nil {
		return 0, err
	}

	modifiedRegs, err = c.ReadRegisters(tid)
	return modifiedRegs.Rcx, err
}

func (c *Client) updateReadTLSFunction(offset uint32) error {
	if c.currentTLSOffset == offset {
		return nil
	}

	readTLSFunction := c.buildReadTLSFunction(offset)
	if err := c.WriteMemory(c.readTLSFuncAddr, readTLSFunction); err != nil {
		return err
	}
	c.currentTLSOffset = offset
	return nil
}

func (c *Client) buildReadTLSFunction(offset uint32) []byte {
	offsetBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(offsetBytes, offset)

	// TODO: do not assume gs_base. fs_base is used in linux.
	readTLSFunction := []byte{0x65, 0x48, 0x8b, 0x0c, 0x25}
	return append(readTLSFunction, offsetBytes...)
}

// ContinueAndWait resumes processes and waits until an event happens.
// The exited event is reported when the main process exits (and not when its threads exit).
func (c *Client) ContinueAndWait() (debugapi.Event, error) {
	return c.continueAndWait(0)
}

// StepAndWait executes the one instruction of the specified thread and waits until an event happens.
// The returned event may not be the trapped event.
// If unspecified thread is stopped, debugapi.UnspecifiedThreadError is returned.
func (c *Client) StepAndWait(threadID int) (debugapi.Event, error) {
	command := fmt.Sprintf("vCont;s:%x", threadID)
	if err := c.send(command); err != nil {
		return debugapi.Event{}, fmt.Errorf("send error: %v", err)
	}

	event, err := c.wait()
	if err != nil {
		return debugapi.Event{}, err
	} else if event.Type != debugapi.EventTypeTrapped {
		return debugapi.Event{}, fmt.Errorf("unexpected event: %#v", event)
	} else if tids := event.Data.([]int); len(tids) != 1 || tids[0] != threadID {
		return debugapi.Event{}, debugapi.UnspecifiedThreadError{ThreadIDs: tids}
	}
	return event, err
}

func (c *Client) continueAndWait(signalNumber int) (debugapi.Event, error) {
	var command string
	if signalNumber == 0 {
		command = "vCont;c"
	} else {
		command = fmt.Sprintf("vCont;C%02x", signalNumber)
	}
	if err := c.send(command); err != nil {
		return debugapi.Event{}, fmt.Errorf("send error: %v", err)
	}

	return c.wait()
}

func (c *Client) wait() (debugapi.Event, error) {
	var data string
	var err error
	for {
		data, err = c.receiveWithTimeout(10 * time.Second)
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// debugserver sometimes does not send a reply packet even when a thread is stopped.
			data, err = c.checkStopReply()
			if err != nil {
				return debugapi.Event{}, fmt.Errorf("failed to query stop reply: %v", err)
			} else if data != "" {
				log.Debugf("debugserver did not reply packets though there is the stopped thread.")
				break
			}
		} else if err != nil {
			return debugapi.Event{}, fmt.Errorf("receive error: %v", err)
		}
		if data != "" {
			break
		}
	}

	stopReplies := c.buildStopReplies(data)
	// process O packet beforehand in order to simplify further processing.
	stopReplies, err = c.processOutputPacket(stopReplies)
	if err != nil {
		return debugapi.Event{}, fmt.Errorf("failed to process output packet: %v", err)
	}
	if len(stopReplies) == 0 {
		return c.wait()
	}
	return c.handleStopReply(stopReplies)
}

func (c *Client) checkStopReply() (string, error) {
	tids, err := c.ThreadIDs()
	if err != nil {
		return "", err
	}

	for _, tid := range tids {
		data, err := c.qThreadStopInfo(tid)
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(data, "T00") {
			return data, nil
		}
	}
	return "", nil
}

func (c *Client) buildStopReplies(data string) []string {
	replies := strings.Split(data, "$")
	for i, reply := range replies {
		if reply[len(reply)-3] == '#' {
			replies[i] = reply[0 : len(reply)-3]
		}
	}
	return replies
}

func (c *Client) processOutputPacket(stopReplies []string) ([]string, error) {
	var unprocessedReplies []string
	for _, stopReply := range stopReplies {
		if stopReply[0] != 'O' {
			unprocessedReplies = append(unprocessedReplies, stopReply)
			continue
		}

		out, err := hexToByteArray(stopReply[1:])
		if err != nil {
			return nil, err
		}
		c.outputWriter.Write(out)
	}
	return unprocessedReplies, nil
}

func (c *Client) handleStopReply(stopReplies []string) (event debugapi.Event, err error) {
	switch stopReplies[0][0] {
	case 'T':
		if len(stopReplies) > 1 {
			log.Debugf("received 2 or more stop replies at once. Consider only first one. data: %v", stopReplies)
		}
		event, err = c.handleTPacket(stopReplies[0])
	case 'W':
		// Ignore remaining packets because the process ends.
		event, err = c.handleWPacket(stopReplies[0])
	case 'X':
		// Ignore remaining packets because the process ends.
		event, err = c.handleXPacket(stopReplies[0])
	default:
		err = fmt.Errorf("unknown packet type: %s", stopReplies[0])
	}
	if err != nil {
		log.Debugf("failed to handle the packet (data: %v): %v", stopReplies[0], err)
		return debugapi.Event{}, err
	}

	if debugapi.IsExitEvent(event.Type) {
		// the connection may be closed already.
		_ = c.close()
	}
	return event, nil
}

func (c *Client) handleTPacket(packet string) (debugapi.Event, error) {
	signalNumber, err := hexToUint64(packet[1:3], false)
	if err != nil {
		return debugapi.Event{}, err
	}
	if syscall.Signal(signalNumber) == excBadAccess {
		return debugapi.Event{}, fmt.Errorf("memory access error")
	}

	var threadIDs []int
	for _, kvInStr := range strings.Split(packet[3:len(packet)-1], ";") {
		kvArr := strings.Split(kvInStr, ":")
		key, value := kvArr[0], kvArr[1]
		if key == "threads" {
			for _, tid := range strings.Split(value, ",") {
				tidInNum, err := hexToUint64(tid, false)
				if err != nil {
					return debugapi.Event{}, err
				}
				threadIDs = append(threadIDs, int(tidInNum))
			}
		}
	}

	trappedThreadIDs, err := c.selectTrappedThreads(threadIDs)
	if err != nil {
		return debugapi.Event{}, err
	} else if len(trappedThreadIDs) == 0 {
		return c.continueAndWait(int(signalNumber))
	}

	return debugapi.Event{Type: debugapi.EventTypeTrapped, Data: trappedThreadIDs}, nil
}

func (c *Client) selectTrappedThreads(tids []int) ([]int, error) {
	var trappedThreads []int
	for _, tid := range tids {
		data, err := c.qThreadStopInfo(tid)
		if err != nil {
			return nil, err
		}

		signalNumber, err := hexToUint64(data[1:3], false)
		if err != nil {
			return nil, err
		}

		if syscall.Signal(signalNumber) == unix.SIGTRAP {
			trappedThreads = append(trappedThreads, tid)
		}
	}
	return trappedThreads, nil
}

func (c *Client) qThreadStopInfo(tid int) (string, error) {
	command := fmt.Sprintf("qThreadStopInfo%02x", tid)
	if err := c.send(command); err != nil {
		return "", err
	}

	data, err := c.receive()
	if err != nil {
		return "", err
	} else if strings.HasPrefix(data, "E") {
		return data, fmt.Errorf("error response: %s", data)
	}
	return data, nil
}

func (c *Client) handleWPacket(packet string) (debugapi.Event, error) {
	exitStatus, err := hexToUint64(packet[1:3], false)
	return debugapi.Event{Type: debugapi.EventTypeExited, Data: int(exitStatus)}, err
}

func (c *Client) handleXPacket(packet string) (debugapi.Event, error) {
	signalNumber, err := hexToUint64(packet[1:3], false)
	// TODO: signalNumber here looks always 0. The number in the description looks correct, so maybe better to use it instead.
	return debugapi.Event{Type: debugapi.EventTypeTerminated, Data: int(signalNumber)}, err
}

func (c *Client) send(command string) error {
	packet := fmt.Sprintf("$%s#00", command)
	if !c.noAckMode {
		packet = fmt.Sprintf("$%s#%02x", command, calcChecksum([]byte(command)))
	}

	if n, err := c.conn.Write([]byte(packet)); err != nil {
		return err
	} else if n != len(packet) {
		return fmt.Errorf("only part of the buffer is sent: %d / %d", n, len(packet))
	}

	if !c.noAckMode {
		return c.receiveAck()
	}
	return nil
}

func (c *Client) receiveAndCheck() error {
	if data, err := c.receive(); err != nil {
		return err
	} else if data != "OK" {
		return fmt.Errorf("the error response is returned: %s", data)
	}

	return nil
}

func (c *Client) receive() (string, error) {
	n, err := c.conn.Read(c.buffer)
	if err != nil {
		return "", err
	}

	packet := string(c.buffer[0:n])
	data := string(packet[1 : n-3])
	if !c.noAckMode {
		if err := verifyPacket(packet); err != nil {
			return "", err
		}

		return data, c.sendAck()
	}

	// quick check
	if packet[n-3] != '#' {
		return data, fmt.Errorf("No checksum. There may be unreceived packets: %s", packet)
	}

	return data, nil
}

func (c *Client) receiveWithTimeout(timeout time.Duration) (string, error) {
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	defer c.conn.SetReadDeadline(time.Time{})

	return c.receive()
}

func (c *Client) sendAck() error {
	_, err := c.conn.Write([]byte("+"))
	return err
}

func (c *Client) receiveAck() error {
	if _, err := c.conn.Read(c.buffer[0:1]); err != nil {
		return err
	} else if c.buffer[0] != '+' {
		return errors.New("failed to receive ack")
	}

	return nil
}

func verifyPacket(packet string) error {
	if packet[0:1] != "$" {
		return fmt.Errorf("invalid head data: %v", packet[0])
	}

	if packet[len(packet)-3:len(packet)-2] != "#" {
		return fmt.Errorf("invalid tail data: %v", packet[len(packet)-3])
	}

	body := packet[1 : len(packet)-3]
	bodyChecksum := strconv.FormatUint(uint64(calcChecksum([]byte(body))), 16)
	tailChecksum := packet[len(packet)-2:]
	if tailChecksum != bodyChecksum {
		return fmt.Errorf("invalid checksum: %s", tailChecksum)
	}

	return nil
}

func hexToUint64(hex string, littleEndian bool) (uint64, error) {
	if littleEndian {
		var reversedHex bytes.Buffer
		for i := len(hex) - 2; i >= 0; i -= 2 {
			reversedHex.WriteString(hex[i : i+2])
		}
		hex = reversedHex.String()
	}
	return strconv.ParseUint(hex, 16, 64)
}

func hexToByteArray(hex string) ([]byte, error) {
	out := make([]byte, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		value, err := strconv.ParseUint(hex[i:i+2], 16, 8)
		if err != nil {
			return nil, err
		}

		out[i/2] = uint8(value)
	}
	return out, nil
}

func uint64ToHex(input uint64, littleEndian bool) string {
	hex := fmt.Sprintf("%016x", input)
	if littleEndian {
		var reversedHex bytes.Buffer
		for i := len(hex) - 2; i >= 0; i -= 2 {
			reversedHex.WriteString(hex[i : i+2])
		}
		hex = reversedHex.String()
	}
	return hex
}

func calcChecksum(buff []byte) uint8 {
	var sum uint8
	for _, b := range buff {
		sum += b
	}
	return sum
}
