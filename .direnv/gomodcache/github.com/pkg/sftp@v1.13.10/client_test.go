package sftp

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/kr/fs"
)

// assert that *Client implements fs.FileSystem
var _ fs.FileSystem = new(Client)

// assert that *File implements io.ReadWriteCloser
var _ io.ReadWriteCloser = new(File)

func TestNormaliseError(t *testing.T) {
	var (
		ok         = &StatusError{Code: sshFxOk}
		eof        = &StatusError{Code: sshFxEOF}
		fail       = &StatusError{Code: sshFxFailure}
		noSuchFile = &StatusError{Code: sshFxNoSuchFile}
		foo        = errors.New("foo")
	)

	var tests = []struct {
		desc string
		err  error
		want error
	}{
		{
			desc: "nil error",
		},
		{
			desc: "not *StatusError",
			err:  foo,
			want: foo,
		},
		{
			desc: "*StatusError with ssh_FX_EOF",
			err:  eof,
			want: io.EOF,
		},
		{
			desc: "*StatusError with ssh_FX_NO_SUCH_FILE",
			err:  noSuchFile,
			want: os.ErrNotExist,
		},
		{
			desc: "*StatusError with ssh_FX_OK",
			err:  ok,
		},
		{
			desc: "*StatusError with ssh_FX_FAILURE",
			err:  fail,
			want: fail,
		},
	}

	for _, tt := range tests {
		got := normaliseError(tt.err)
		if got != tt.want {
			t.Errorf("normaliseError(%#v), test %q\n- want: %#v\n-  got: %#v",
				tt.err, tt.desc, tt.want, got)
		}
	}
}

var flagsTests = []struct {
	flags int
	want  uint32
}{
	{os.O_RDONLY, sshFxfRead},
	{os.O_WRONLY, sshFxfWrite},
	{os.O_RDWR, sshFxfRead | sshFxfWrite},
	{os.O_RDWR | os.O_CREATE | os.O_TRUNC, sshFxfRead | sshFxfWrite | sshFxfCreat | sshFxfTrunc},
	{os.O_WRONLY | os.O_APPEND, sshFxfWrite | sshFxfAppend},
}

func TestFlags(t *testing.T) {
	for i, tt := range flagsTests {
		got := toPflags(tt.flags)
		if got != tt.want {
			t.Errorf("test %v: flags(%x): want: %x, got: %x", i, tt.flags, tt.want, got)
		}
	}
}

type packetSizeTest struct {
	size  int
	valid bool
}

var maxPacketCheckedTests = []packetSizeTest{
	{size: 0, valid: false},
	{size: 1, valid: true},
	{size: 32768, valid: true},
	{size: 32769, valid: false},
}

var maxPacketUncheckedTests = []packetSizeTest{
	{size: 0, valid: false},
	{size: 1, valid: true},
	{size: 32768, valid: true},
	{size: 32769, valid: true},
}

func TestMaxPacketChecked(t *testing.T) {
	for _, tt := range maxPacketCheckedTests {
		testMaxPacketOption(t, MaxPacketChecked(tt.size), tt)
	}
}

func TestMaxPacketUnchecked(t *testing.T) {
	for _, tt := range maxPacketUncheckedTests {
		testMaxPacketOption(t, MaxPacketUnchecked(tt.size), tt)
	}
}

func TestMaxPacket(t *testing.T) {
	for _, tt := range maxPacketCheckedTests {
		testMaxPacketOption(t, MaxPacket(tt.size), tt)
	}
}

func testMaxPacketOption(t *testing.T, o ClientOption, tt packetSizeTest) {
	var c Client

	err := o(&c)
	if (err == nil) != tt.valid {
		t.Errorf("MaxPacketChecked(%v)\n- want: %v\n- got: %v", tt.size, tt.valid, err == nil)
	}
	if c.maxPacket != tt.size && tt.valid {
		t.Errorf("MaxPacketChecked(%v)\n- want: %v\n- got: %v", tt.size, tt.size, c.maxPacket)
	}
}

func testFstatOption(t *testing.T, o ClientOption, value bool) {
	var c Client

	err := o(&c)
	if err == nil && c.useFstat != value {
		t.Errorf("UseFStat(%v)\n- want: %v\n- got: %v", value, value, c.useFstat)
	}
}

func TestUseFstatChecked(t *testing.T) {
	testFstatOption(t, UseFstat(true), true)
	testFstatOption(t, UseFstat(false), false)
}

type sink struct{}

func (*sink) Close() error                { return nil }
func (*sink) Write(p []byte) (int, error) { return len(p), nil }

func TestClientZeroLengthPacket(t *testing.T) {
	// Packet length zero (never valid). This used to crash the client.
	packet := []byte{0, 0, 0, 0}

	r := bytes.NewReader(packet)
	c, err := NewClientPipe(r, &sink{})
	if err == nil {
		t.Error("expected an error, got nil")
	}
	if c != nil {
		c.Close()
	}
}

func TestClientShortPacket(t *testing.T) {
	// init packet too short.
	packet := []byte{0, 0, 0, 1, 2}

	r := bytes.NewReader(packet)
	_, err := NewClientPipe(r, &sink{})
	if !errors.Is(err, errShortPacket) {
		t.Fatalf("expected error: %v, got: %v", errShortPacket, err)
	}
}

// Issue #418: panic in clientConn.recv when the sid is incomplete.
func TestClientNoSid(t *testing.T) {
	stream := new(bytes.Buffer)
	sendPacket(stream, &sshFxVersionPacket{Version: sftpProtocolVersion})
	// Next packet has the sid cut short after two bytes.
	stream.Write([]byte{0, 0, 0, 10, 0, 0})

	c, err := NewClientPipe(stream, &sink{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Stat("anything")
	if !errors.Is(err, ErrSSHFxConnectionLost) {
		t.Fatal("expected ErrSSHFxConnectionLost, got", err)
	}
}
