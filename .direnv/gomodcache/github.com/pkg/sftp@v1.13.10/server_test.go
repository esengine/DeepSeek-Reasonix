package sftp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"runtime"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clientServerPair(t *testing.T) (*Client, *Server) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	var options []ServerOption
	if *testAllocator {
		options = append(options, WithAllocator())
	}
	server, err := NewServer(struct {
		io.Reader
		io.WriteCloser
	}{sr, sw}, options...)
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve()
	client, err := NewClientPipe(cr, cw)
	if err != nil {
		t.Fatalf("%+v\n", err)
	}
	return client, server
}

type sshFxpTestBadExtendedPacket struct {
	ID        uint32
	Extension string
	Data      string
}

func (p sshFxpTestBadExtendedPacket) id() uint32 { return p.ID }

func (p sshFxpTestBadExtendedPacket) MarshalBinary() ([]byte, error) {
	l := 4 + 1 + 4 + // uint32(length) + byte(type) + uint32(id)
		4 + len(p.Extension) +
		4 + len(p.Data)

	b := make([]byte, 4, l)
	b = append(b, sshFxpExtended)
	b = marshalUint32(b, p.ID)
	b = marshalString(b, p.Extension)
	b = marshalString(b, p.Data)

	return b, nil
}

func checkServerAllocator(t *testing.T, server *Server) {
	if server.pktMgr.alloc == nil {
		return
	}
	checkAllocatorBeforeServerClose(t, server.pktMgr.alloc)
	server.Close()
	checkAllocatorAfterServerClose(t, server.pktMgr.alloc)
}

// test that errors are sent back when we request an invalid extended packet operation
// this validates the following rfc draft is followed https://tools.ietf.org/html/draft-ietf-secsh-filexfer-extensions-00
func TestInvalidExtendedPacket(t *testing.T) {
	client, server := clientServerPair(t)
	defer client.Close()
	defer server.Close()

	badPacket := sshFxpTestBadExtendedPacket{client.nextID(), "thisDoesn'tExist", "foobar"}
	typ, data, err := client.clientConn.sendPacket(context.Background(), nil, badPacket)
	if err != nil {
		t.Fatalf("unexpected error from sendPacket: %s", err)
	}
	if typ != sshFxpStatus {
		t.Fatalf("received non-FPX_STATUS packet: %v", typ)
	}

	err = unmarshalStatus(badPacket.id(), data)
	statusErr, ok := err.(*StatusError)
	if !ok {
		t.Fatal("failed to convert error from unmarshalStatus to *StatusError")
	}
	if statusErr.Code != sshFxOPUnsupported {
		t.Errorf("statusErr.Code => %d, wanted %d", statusErr.Code, sshFxOPUnsupported)
	}
	checkServerAllocator(t, server)
}

// test that server handles concurrent requests correctly
func TestConcurrentRequests(t *testing.T) {
	skipIfWindows(t)
	var filename string
	switch runtime.GOOS {
	case "plan9":
		filename = "/lib/ndb/local"
	case "zos":
		filename = "/etc/.shrc"
	default:
		filename = "/etc/passwd"
	}
	client, server := clientServerPair(t)
	defer client.Close()
	defer server.Close()

	concurrency := 2
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()

			for j := 0; j < 1024; j++ {
				f, err := client.Open(filename)
				if err != nil {
					t.Errorf("failed to open file: %v", err)
					continue
				}
				if err := f.Close(); err != nil {
					t.Errorf("failed t close file: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	checkServerAllocator(t, server)
}

// Test error conversion
func TestStatusFromError(t *testing.T) {
	type test struct {
		err error
		pkt *sshFxpStatusPacket
	}
	tpkt := func(id, code uint32) *sshFxpStatusPacket {
		return &sshFxpStatusPacket{
			ID:          id,
			StatusError: StatusError{Code: code},
		}
	}
	testCases := []test{
		{syscall.ENOENT, tpkt(1, sshFxNoSuchFile)},
		{&os.PathError{Err: syscall.ENOENT},
			tpkt(2, sshFxNoSuchFile)},
		{&os.PathError{Err: errors.New("foo")}, tpkt(3, sshFxFailure)},
		{ErrSSHFxEOF, tpkt(4, sshFxEOF)},
		{ErrSSHFxOpUnsupported, tpkt(5, sshFxOPUnsupported)},
		{io.EOF, tpkt(6, sshFxEOF)},
		{os.ErrNotExist, tpkt(7, sshFxNoSuchFile)},
	}
	for _, tc := range testCases {
		tc.pkt.StatusError.msg = tc.err.Error()
		assert.Equal(t, tc.pkt, statusFromError(tc.pkt.ID, tc.err))
	}
}

// This was written to test a race b/w open immediately followed by a stat.
// Previous to this the Open would trigger the use of a worker pool, then the
// stat packet would come in an hit the pool and return faster than the open
// (returning a file-not-found error).
// The below by itself wouldn't trigger the race however, I needed to add a
// small sleep in the openpacket code to trigger the issue. I wanted to add a
// way to inject that in the code but right now there is no good place for it.
// I'm thinking after I convert the server into a request-server backend I
// might be able to do something with the runWorker method passed into the
// packet manager. But with the 2 implementations fo the server it just doesn't
// fit well right now.
func TestOpenStatRace(t *testing.T) {
	client, server := clientServerPair(t)
	defer client.Close()
	defer server.Close()

	// openpacket finishes to fast to trigger race in tests
	// need to add a small sleep on server to openpackets somehow
	tmppath := path.Join(os.TempDir(), "stat_race")
	pflags := toPflags(os.O_RDWR | os.O_CREATE | os.O_TRUNC)
	ch := make(chan result, 3)
	id1 := client.nextID()
	id2 := client.nextID()
	client.dispatchRequest(ch, &sshFxpOpenPacket{
		ID:     id1,
		Path:   tmppath,
		Pflags: pflags,
	})
	client.dispatchRequest(ch, &sshFxpLstatPacket{
		ID:   id2,
		Path: tmppath,
	})
	testreply := func(id uint32) {
		r := <-ch
		require.NoError(t, r.err)
		switch r.typ {
		case sshFxpAttrs, sshFxpHandle: // ignore
		case sshFxpStatus:
			err := normaliseError(unmarshalStatus(id, r.data))
			assert.NoError(t, err, "race hit, stat before open")
		default:
			t.Fatal("unexpected type:", r.typ)
		}
	}
	testreply(id1)
	testreply(id2)
	os.Remove(tmppath)
	checkServerAllocator(t, server)
}

func TestOpenWithPermissions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	skipIfWindows(t)

	client, server := clientServerPair(t)
	defer client.Close()
	defer server.Close()

	tmppath := path.Join(os.TempDir(), "open_permissions")
	defer os.Remove(tmppath)

	pflags := toPflags(os.O_RDWR | os.O_CREATE | os.O_TRUNC)

	id1 := client.nextID()
	id2 := client.nextID()

	typ, data, err := client.sendPacket(ctx, nil, &sshFxpOpenPacket{
		ID:     id1,
		Path:   tmppath,
		Pflags: pflags,
		Flags:  sshFileXferAttrPermissions,
		Attrs: &FileStat{
			Mode: 0o745,
		},
	})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	switch typ {
	case sshFxpHandle:
		// do nothing, we can just leave the handle open.
	case sshFxpStatus:
		t.Fatal("unexpected status:", normaliseError(unmarshalStatus(id1, data)))
	default:
		t.Fatal("unpexpected packet type:", unimplementedPacketErr(typ))
	}

	stat, err := os.Stat(tmppath)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if stat.Mode()&os.ModePerm != 0o745 {
		t.Errorf("stat.Mode() = %v was expecting 0o745", stat.Mode())
	}

	// Existing files should not have their permissions changed.
	typ, data, err = client.sendPacket(ctx, nil, &sshFxpOpenPacket{
		ID:     id2,
		Path:   tmppath,
		Pflags: pflags,
		Flags:  sshFileXferAttrPermissions,
		Attrs: &FileStat{
			Mode: 0o755,
		},
	})
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	switch typ {
	case sshFxpHandle:
		// do nothing, we can just leave the handle open.
	case sshFxpStatus:
		t.Fatal("unexpected status:", normaliseError(unmarshalStatus(id2, data)))
	default:
		t.Fatal("unpexpected packet type:", unimplementedPacketErr(typ))
	}

	if stat.Mode()&os.ModePerm != 0o745 {
		t.Errorf("stat.Mode() = %v, was expecting unchanged 0o745", stat.Mode())
	}

	checkServerAllocator(t, server)
}

// Ensure that proper error codes are returned for non existent files, such
// that they are mapped back to a 'not exists' error on the client side.
func TestStatNonExistent(t *testing.T) {
	client, server := clientServerPair(t)
	defer client.Close()
	defer server.Close()

	for _, file := range []string{"/doesnotexist", "/doesnotexist/a/b"} {
		_, err := client.Stat(file)
		if !os.IsNotExist(err) {
			t.Errorf("expected 'does not exist' err for file %q.  got: %v", file, err)
		}
	}
}

func TestServerWithBrokenClient(t *testing.T) {
	validInit := sp(&sshFxInitPacket{Version: 3})
	brokenOpen := sp(&sshFxpOpenPacket{Path: "foo"})
	brokenOpen = brokenOpen[:len(brokenOpen)-2]

	for _, clientInput := range [][]byte{
		// Packet length zero (never valid). This used to crash the server.
		{0, 0, 0, 0},
		append(validInit, 0, 0, 0, 0),

		// Client hangs up mid-packet.
		append(validInit, brokenOpen...),
	} {
		srv, err := NewServer(struct {
			io.Reader
			io.WriteCloser
		}{
			bytes.NewReader(clientInput),
			&sink{},
		})
		require.NoError(t, err)

		err = srv.Serve()
		assert.Error(t, err)
		srv.Close()
	}
}
