package sftp

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testHandler struct {
	filecontents []byte      // dummy contents
	output       io.WriterAt // dummy file out
	err          error       // dummy error, should be file related
}

func (t *testHandler) Fileread(r *Request) (io.ReaderAt, error) {
	if t.err != nil {
		return nil, t.err
	}
	_ = r.WithContext(r.Context()) // initialize context for deadlock testing
	return bytes.NewReader(t.filecontents), nil
}

func (t *testHandler) Filewrite(r *Request) (io.WriterAt, error) {
	if t.err != nil {
		return nil, t.err
	}
	_ = r.WithContext(r.Context()) // initialize context for deadlock testing
	return io.WriterAt(t.output), nil
}

func (t *testHandler) Filecmd(r *Request) error {
	_ = r.WithContext(r.Context()) // initialize context for deadlock testing
	return t.err
}

func (t *testHandler) Filelist(r *Request) (ListerAt, error) {
	if t.err != nil {
		return nil, t.err
	}
	_ = r.WithContext(r.Context()) // initialize context for deadlock testing
	f, err := os.Open(r.Filepath)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return listerat([]os.FileInfo{fi}), nil
}

// make sure len(fakefile) == len(filecontents)
type fakefile [10]byte

var filecontents = []byte("file-data.")

// XXX need new for creating test requests that supports Open-ing
func testRequest(method string) *Request {
	var flags uint32
	switch method {
	case "Get":
		flags = flags | sshFxfRead
	case "Put":
		flags = flags | sshFxfWrite
	}
	request := &Request{
		Filepath: "./request_test.go",
		Method:   method,
		Attrs:    []byte("foo"),
		Flags:    flags,
		Target:   "foo",
	}
	return request
}

func (ff *fakefile) WriteAt(p []byte, off int64) (int, error) {
	n := copy(ff[off:], p)
	return n, nil
}

func (ff fakefile) string() string {
	b := make([]byte, len(ff))
	copy(b, ff[:])
	return string(b)
}

func newTestHandlers() Handlers {
	handler := &testHandler{
		filecontents: filecontents,
		output:       &fakefile{},
		err:          nil,
	}
	return Handlers{
		FileGet:  handler,
		FilePut:  handler,
		FileCmd:  handler,
		FileList: handler,
	}
}

func (h Handlers) getOutString() string {
	handler := h.FilePut.(*testHandler)
	return handler.output.(*fakefile).string()
}

var errTest = errors.New("test error")

func (h *Handlers) returnError(err error) {
	handler := h.FilePut.(*testHandler)
	handler.err = err
}

func getStatusMsg(p interface{}) string {
	pkt := p.(*sshFxpStatusPacket)
	return pkt.StatusError.msg
}
func checkOkStatus(t *testing.T, p interface{}) {
	pkt := p.(*sshFxpStatusPacket)
	assert.Equal(t, pkt.StatusError.Code, uint32(sshFxOk),
		"sshFxpStatusPacket not OK\n", pkt.StatusError.msg)
}

// fake/test packet
type fakePacket struct {
	myid   uint32
	handle string
}

func (f fakePacket) id() uint32 {
	return f.myid
}

func (f fakePacket) getHandle() string {
	return f.handle
}
func (fakePacket) UnmarshalBinary(d []byte) error { return nil }

// XXX can't just set method to Get, need to use Open to setup Get/Put
func TestRequestGet(t *testing.T) {
	handlers := newTestHandlers()
	request := testRequest("Get")
	pkt := fakePacket{myid: 1}
	request.open(handlers, pkt)
	// req.length is 5, so we test reads in 5 byte chunks
	for i, txt := range []string{"file-", "data."} {
		pkt := &sshFxpReadPacket{ID: uint32(i), Handle: "a",
			Offset: uint64(i * 5), Len: 5}
		rpkt := request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
		dpkt := rpkt.(*sshFxpDataPacket)
		assert.Equal(t, dpkt.id(), uint32(i))
		assert.Equal(t, string(dpkt.Data), txt)
	}
}

func TestRequestCustomError(t *testing.T) {
	handlers := newTestHandlers()
	request := testRequest("Stat")
	pkt := fakePacket{myid: 1}
	cmdErr := errors.New("stat not supported")
	handlers.returnError(cmdErr)
	rpkt := request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	assert.Equal(t, rpkt, statusFromError(pkt.myid, cmdErr))
}

// XXX can't just set method to Get, need to use Open to setup Get/Put
func TestRequestPut(t *testing.T) {
	handlers := newTestHandlers()
	request := testRequest("Put")
	request.state.writerAt, _ = handlers.FilePut.Filewrite(request)
	pkt := &sshFxpWritePacket{ID: 0, Handle: "a", Offset: 0, Length: 5,
		Data: []byte("file-")}
	rpkt := request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	checkOkStatus(t, rpkt)
	pkt = &sshFxpWritePacket{ID: 1, Handle: "a", Offset: 5, Length: 5,
		Data: []byte("data.")}
	rpkt = request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	checkOkStatus(t, rpkt)
	assert.Equal(t, "file-data.", handlers.getOutString())
}

func TestRequestCmdr(t *testing.T) {
	handlers := newTestHandlers()
	request := testRequest("Mkdir")
	pkt := fakePacket{myid: 1}
	rpkt := request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	checkOkStatus(t, rpkt)

	handlers.returnError(errTest)
	rpkt = request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	assert.Equal(t, rpkt, statusFromError(pkt.myid, errTest))
}

func TestRequestInfoStat(t *testing.T) {
	handlers := newTestHandlers()
	request := testRequest("Stat")
	pkt := fakePacket{myid: 1}
	rpkt := request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	spkt, ok := rpkt.(*sshFxpStatResponse)
	assert.True(t, ok)
	assert.Equal(t, spkt.info.Name(), "request_test.go")
}

func TestRequestInfoList(t *testing.T) {
	handlers := newTestHandlers()
	request := testRequest("List")
	request.handle = "1"
	pkt := fakePacket{myid: 1}
	rpkt := request.opendir(handlers, pkt)
	hpkt, ok := rpkt.(*sshFxpHandlePacket)
	if assert.True(t, ok) {
		assert.Equal(t, hpkt.Handle, "1")
	}
	pkt = fakePacket{myid: 2}
	request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
}
func TestRequestInfoReadlink(t *testing.T) {
	handlers := newTestHandlers()
	request := testRequest("Readlink")
	pkt := fakePacket{myid: 1}
	rpkt := request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	npkt, ok := rpkt.(*sshFxpNamePacket)
	if assert.True(t, ok) {
		assert.IsType(t, &sshFxpNameAttr{}, npkt.NameAttrs[0])
		assert.Equal(t, npkt.NameAttrs[0].Name, "request_test.go")
	}
}

func TestOpendirHandleReuse(t *testing.T) {
	handlers := newTestHandlers()
	request := testRequest("Stat")
	request.handle = "1"
	pkt := fakePacket{myid: 1}
	rpkt := request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	assert.IsType(t, &sshFxpStatResponse{}, rpkt)

	request.Method = "List"
	pkt = fakePacket{myid: 2}
	rpkt = request.opendir(handlers, pkt)
	if assert.IsType(t, &sshFxpHandlePacket{}, rpkt) {
		hpkt := rpkt.(*sshFxpHandlePacket)
		assert.Equal(t, hpkt.Handle, "1")
	}
	rpkt = request.call(handlers, pkt, nil, 0, defaultMaxTxPacket)
	assert.IsType(t, &sshFxpNamePacket{}, rpkt)
}
