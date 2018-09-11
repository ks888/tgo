package lldb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/ks888/tgo/debugapi"
	"golang.org/x/sys/unix"
)

const debugServerPath = "/Library/Developer/CommandLineTools/Library/PrivateFrameworks/LLDB.framework/Versions/A/Resources/debugserver"

// Assumes the packet size is not larger than this.
const maxPacketSize = 4096

// Client is the debug api client which depends on lldb's debugserver.
// See the gdb's doc for the reference: https://sourceware.org/gdb/onlinedocs/gdb/Remote-Protocol.html
// Some commands use the lldb extension: https://github.com/llvm-mirror/lldb/blob/master/docs/lldb-gdb-remote.txt
type Client struct {
	conn                 net.Conn
	pid                  int
	noAckMode            bool
	registerMetadataList []registerMetadata
	buffer               []byte

	readTLSFuncAddr uint64
	currentOffset   uint32
}

// NewClient returns the new debug api client which depends on OS API.
func NewClient() *Client {
	return &Client{buffer: make([]byte, maxPacketSize)}
}

// LaunchProcess lets the debugserver launch the new prcoess.
func (c *Client) LaunchProcess(name string, arg ...string) (int, error) {
	listener, err := net.Listen("tcp", "localhost:")
	if err != nil {
		return 0, err
	}

	debugServerArgs := []string{"-F", "-R", listener.Addr().String(), "--", name}
	debugServerArgs = append(debugServerArgs, arg...)
	cmd := exec.Command(debugServerPath, debugServerArgs...)
	if err := cmd.Start(); err != nil {
		return 0, err
	}

	c.conn, err = c.waitConnectOrExit(listener, cmd)
	if err != nil {
		return 0, err
	}
	c.pid = cmd.Process.Pid

	if err := c.initialize(); err != nil {
		return 0, err
	}

	return c.firstTid()
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

func (c *Client) firstTid() (int, error) {
	tids, err := c.qfThreadInfo()
	if err != nil {
		return 0, err
	}
	tid, err := hexToUint64(strings.Split(tids, ",")[0], false)
	return int(tid), err
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

	return data[1:len(data)], nil
}

// AttachProcess lets the debugserver attach the new prcoess.
func (c *Client) AttachProcess(pid int) (int, error) {
	listener, err := net.Listen("tcp", "localhost:")
	if err != nil {
		return 0, err
	}

	debugServerArgs := []string{"-F", "-R", listener.Addr().String(), fmt.Sprintf("--attach=%d", pid)}
	cmd := exec.Command(debugServerPath, debugServerArgs...)
	if err := cmd.Start(); err != nil {
		return 0, err
	}

	c.conn, err = c.waitConnectOrExit(listener, cmd)
	if err != nil {
		return 0, err
	}
	c.pid = cmd.Process.Pid

	if err := c.initialize(); err != nil {
		return 0, err
	}

	return c.firstTid()
}

// DetachProcess detaches from the prcoess.
func (c *Client) DetachProcess() error {
	if err := c.send("D"); err != nil {
		return err
	}

	return c.receiveAndCheck()
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
// The 'P' command is not used here due to the bug explained here: https://github.com/llvm-mirror/lldb/commit/d8d7a40ca5377aa777e3840f3e9b6a63c6b09445
func (c *Client) WriteRegisters(tid int, regs debugapi.Registers) error {
	data, err := c.readRegisters(tid)
	if err != nil {
		return err
	}

	for _, metadata := range c.registerMetadataList {
		prefix := data[0 : metadata.offset*2]
		suffix := data[(metadata.offset+metadata.size)*2 : len(data)]

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

	for i := 0; i < len(data); i += 2 {
		value, err := strconv.ParseUint(data[i:i+2], 16, 8)
		if err != nil {
			return err
		}

		out[i/2] = uint8(value)
	}

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

	if _, _, err = c.StepAndWait(tid); err != nil {
		return 0, err
	}

	modifiedRegs, err = c.ReadRegisters(tid)
	return modifiedRegs.Rcx, err
}

func (c *Client) updateReadTLSFunction(offset uint32) error {
	if c.currentOffset == offset {
		return nil
	}

	readTLSFunction := c.buildReadTLSFunction(offset)
	if err := c.WriteMemory(c.readTLSFuncAddr, readTLSFunction); err != nil {
		return err
	}
	c.currentOffset = offset
	return nil
}

func (c *Client) buildReadTLSFunction(offset uint32) []byte {
	offsetBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(offsetBytes, offset)

	// TODO: do not assume gs_base. fs_base is used in linux.
	readTLSFunction := []byte{0x65, 0x48, 0x8b, 0x0c, 0x25}
	return append(readTLSFunction, offsetBytes...)
}

// ContinueAndWait resumes the list of processes and waits until an event happens.
// The exited event is reported when the main process exits and not when its threads exit.
func (c *Client) ContinueAndWait() (int, debugapi.Event, error) {
	return c.continueAndWait(0)
}

func (c *Client) StepAndWait(threadID int) (int, debugapi.Event, error) {
	command := fmt.Sprintf("vCont;s:%x", threadID)
	if err := c.send(command); err != nil {
		return 0, debugapi.Event{}, fmt.Errorf("send error: %v", err)
	}

	data, err := c.receive()
	if err != nil {
		return 0, debugapi.Event{}, fmt.Errorf("receive error: %v", err)
	}

	return c.handleStopReply(data)
}

func (c *Client) continueAndWait(signalNumber int) (int, debugapi.Event, error) {
	var command string
	if signalNumber == 0 {
		command = "vCont;c"
	} else {
		command = fmt.Sprintf("vCont;C%02x", signalNumber)
	}
	if err := c.send(command); err != nil {
		return 0, debugapi.Event{}, fmt.Errorf("send error: %v", err)
	}

	data, err := c.receive()
	if err != nil {
		return 0, debugapi.Event{}, fmt.Errorf("receive error: %v", err)
	}

	return c.handleStopReply(data)
}

func (c *Client) handleStopReply(data string) (int, debugapi.Event, error) {
	switch data[0] {
	case 'T':
		return c.handleTPacket(data)
	case 'O':
		// console output
		return c.ContinueAndWait()
	case 'W':
		return c.handleWPacket(data)
	case 'X':
		return c.handleXPacket(data)
	}

	return 0, debugapi.Event{}, fmt.Errorf("unknown packet type: %s", data)
}

func (c *Client) handleTPacket(data string) (int, debugapi.Event, error) {
	signalNumber, err := hexToUint64(data[1:3], false)
	if err != nil {
		return 0, debugapi.Event{}, err
	}

	var threadID int
	for _, kvInStr := range strings.Split(data[3:len(data)-1], ";") {
		kvArr := strings.Split(kvInStr, ":")
		key, value := kvArr[0], kvArr[1]
		if key == "thread" {
			valueInNum, err := hexToUint64(value, false)
			if err != nil {
				return 0, debugapi.Event{}, err
			}
			threadID = int(valueInNum)
			break
		}
	}

	switch syscall.Signal(signalNumber) {
	case unix.SIGTRAP:
		return threadID, debugapi.Event{Type: debugapi.EventTypeTrapped}, nil
	default:
		return c.continueAndWait(int(signalNumber))
	}
}

func (c *Client) handleWPacket(data string) (int, debugapi.Event, error) {
	exitStatus, err := hexToUint64(data[1:3], false)
	// TODO: set pid.
	return 0, debugapi.Event{Type: debugapi.EventTypeExited, Data: int(exitStatus)}, err
}

func (c *Client) handleXPacket(data string) (int, debugapi.Event, error) {
	signalNumber, err := hexToUint64(data[1:3], false)
	// TODO: set pid.
	// TODO: signalNumber here looks always 0. The number in the description looks correct, so use it.
	return 0, debugapi.Event{Type: debugapi.EventTypeTerminated, Data: int(signalNumber)}, err
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

	return data, nil
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
	tailChecksum := packet[len(packet)-2 : len(packet)]
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
