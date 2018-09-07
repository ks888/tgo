package lldb

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/ks888/tgo/debugapi"
)

const debugServerPath = "/Library/Developer/CommandLineTools/Library/PrivateFrameworks/LLDB.framework/Versions/A/Resources/debugserver"

// Assumes the packet size is not larger than this.
// TODO: use the PacketSize the qSupported query tells us.
const maxPacketSize = 256

// Client is the debug api client which depends on lldb's debugserver.
type Client struct {
	buildRegisterList func(rawData []byte) (*debugapi.Registers, error)
	conn              net.Conn
	buffer            []byte
	noAckMode         bool
}

// NewClient returns the new debug api client which depends on OS API.
func NewClient() *Client {
	return &Client{buffer: make([]byte, maxPacketSize)}
}

func (c *Client) setNoAckMode() error {
	const command = "QStartNoAckMode"
	if err := c.send(command); err != nil {
		return err
	}

	if data, err := c.receive(); err != nil {
		return err
	} else if data != "OK" {
		return fmt.Errorf("the error response is returned: %s", data)
	}

	c.noAckMode = true
	return nil
}

func (c *Client) send(command string) error {
	packet := fmt.Sprintf("$%s#%x", command, calcChecksum([]byte(command)))

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

func (c *Client) receive() (string, error) {
	n, err := c.conn.Read(c.buffer)
	if err != nil {
		return "", err
	}

	packet := string(c.buffer[0:n])
	if err := verifyPacket(packet); err != nil {
		return "", err
	}

	data := string(packet[1 : n-3])
	if !c.noAckMode {
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

func decodeHexArray(hexArray string) ([]byte, error) {
	if len(hexArray)%2 != 0 {
		return nil, fmt.Errorf("invalid data: %s", hexArray)
	}

	var values []byte
	for i := 0; i < len(hexArray); i += 2 {
		hex := hexArray[i : i+2]
		value, err := strconv.ParseUint(hex, 16, 8)
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
