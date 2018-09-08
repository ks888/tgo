package lldb

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ks888/tgo/debugapi"
)

const debugServerPath = "/Library/Developer/CommandLineTools/Library/PrivateFrameworks/LLDB.framework/Versions/A/Resources/debugserver"

// Assumes the packet size is not larger than this.
const maxPacketSize = 4096

// Client is the debug api client which depends on lldb's debugserver.
// See the gdb's doc for the reference: https://sourceware.org/gdb/onlinedocs/gdb/Remote-Protocol.html
// Some commands use the lldb extension: https://github.com/llvm-mirror/lldb/blob/master/docs/lldb-gdb-remote.txt
type Client struct {
	registerMetadataList []registerMetadata
	conn                 net.Conn
	buffer               []byte
	noAckMode            bool
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

	if err := c.initialize(); err != nil {
		return 0, err
	}

	return c.firstTid()
}

// ReadRegisters reads the target tid's registers.
func (c *Client) ReadRegisters(tid int) (debugapi.Registers, error) {
	data, err := c.readRegisters(tid)
	if err != nil {
		return debugapi.Registers{}, err
	}

	// TODO: handle data starts with 'E'

	return c.parseRegisterData(data)
}

func (c *Client) readRegisters(tid int) (string, error) {
	command := fmt.Sprintf("g;thread:%x;", tid)
	if err := c.send(command); err != nil {
		return "", err
	}

	return c.receive()
}

func (c *Client) parseRegisterData(data string) (debugapi.Registers, error) {
	var regs debugapi.Registers
	for _, metadata := range c.registerMetadataList {
		rawValue := data[metadata.offset*2 : (metadata.offset+metadata.size)*2]

		var err error
		switch metadata.name {
		case "rip":
			regs.Rip, err = hexToUint64(rawValue)
		case "rsp":
			regs.Rsp, err = hexToUint64(rawValue)
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
			data = fmt.Sprintf("%s%016x%s", prefix, regs.Rip, suffix)
		case "rsp":
			data = fmt.Sprintf("%s%016x%s", prefix, regs.Rsp, suffix)
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

	return c.qListThreadsInStopReply()
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
		return registerMetadata{}, fmt.Errorf("unknown error code: %s", data)
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

func (c *Client) firstTid() (int, error) {
	tidInHex, err := c.qfThreadInfo()
	if err != nil {
		return 0, err
	}
	tid, err := hexToUint64(tidInHex)
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

func (c *Client) send(command string) error {
	packet := fmt.Sprintf("$%s#%02x", command, calcChecksum([]byte(command)))

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

func hexToUint64(hex string) (uint64, error) {
	return strconv.ParseUint(hex, 16, 64)
}

func hexToBytes(hex string) ([]byte, error) {
	if len(hex)%2 != 0 {
		return nil, fmt.Errorf("invalid data: %s", hex)
	}

	var values []byte
	for i := 0; i < len(hex); i += 2 {
		value, err := strconv.ParseUint(hex[i:i+2], 16, 8)
		if err != nil {
			return nil, err
		}

		values = append(values, uint8(value))
	}

	return values, nil
}

func calcChecksum(buff []byte) uint8 {
	var sum uint8
	for _, b := range buff {
		sum += b
	}
	return sum
}
