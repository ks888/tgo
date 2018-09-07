package lldb

import (
	"net"
	"reflect"
	"testing"
)

var (
	infloopProgram           = "testdata/infloop"
	beginTextSection uintptr = 0x1000000
	mainAddr         uintptr = 0x1051430
)

// TODO: terminate process

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

func TestDecodeHexArray(t *testing.T) {
	for i, test := range []struct {
		data     string
		expected []byte
	}{
		{data: "aa", expected: []byte{0xaa}},
		{data: "aabb", expected: []byte{0xaa, 0xbb}},
		{data: "", expected: nil},
	} {
		actual, err := decodeHexArray(test.data)
		if err != nil {
			t.Fatalf("error returned: %v", err)
		}
		if !reflect.DeepEqual(actual, test.expected) {
			t.Errorf("[%d] failed to decode hex array: %v, %v", i, actual, test.expected)
		}
	}
}

func TestDecodeHexArray_NotHexArray(t *testing.T) {
	_, err := decodeHexArray("zz")
	if err == nil {
		t.Fatalf("error not returned: %v", err)
	}
}

func TestDecodeHexArray_TooShortArray(t *testing.T) {
	_, err := decodeHexArray("z")
	if err == nil {
		t.Fatalf("error not returned: %v", err)
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
