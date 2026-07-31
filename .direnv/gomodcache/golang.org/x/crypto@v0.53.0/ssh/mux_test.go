// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

func muxPair() (*mux, *mux) {
	a, b := memPipe()

	s := newMux(a)
	c := newMux(b)

	return s, c
}

// Returns both ends of a channel, and the mux for the 2nd
// channel.
func channelPair(t *testing.T) (*channel, *channel, *mux) {
	c, s := muxPair()

	res := make(chan *channel, 1)
	go func() {
		newCh, ok := <-s.incomingChannels
		if !ok {
			t.Error("no incoming channel")
			close(res)
			return
		}
		if newCh.ChannelType() != "chan" {
			t.Errorf("got type %q want chan", newCh.ChannelType())
			newCh.Reject(Prohibited, fmt.Sprintf("got type %q want chan", newCh.ChannelType()))
			close(res)
			return
		}
		ch, _, err := newCh.Accept()
		if err != nil {
			t.Errorf("accept: %v", err)
			close(res)
			return
		}
		res <- ch.(*channel)
	}()

	ch, err := c.openChannel("chan", nil)
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	w := <-res
	if w == nil {
		t.Fatal("unable to get write channel")
	}

	return w, ch, c
}

// Test that stderr and stdout can be addressed from different
// goroutines. This is intended for use with the race detector.
func TestMuxChannelExtendedThreadSafety(t *testing.T) {
	writer, reader, mux := channelPair(t)
	defer writer.Close()
	defer reader.Close()
	defer mux.Close()

	var wr, rd sync.WaitGroup
	magic := "hello world"

	wr.Add(2)
	go func() {
		io.WriteString(writer, magic)
		wr.Done()
	}()
	go func() {
		io.WriteString(writer.Stderr(), magic)
		wr.Done()
	}()

	rd.Add(2)
	go func() {
		c, err := io.ReadAll(reader)
		if string(c) != magic {
			t.Errorf("stdout read got %q, want %q (error %s)", c, magic, err)
		}
		rd.Done()
	}()
	go func() {
		c, err := io.ReadAll(reader.Stderr())
		if string(c) != magic {
			t.Errorf("stderr read got %q, want %q (error %s)", c, magic, err)
		}
		rd.Done()
	}()

	wr.Wait()
	writer.CloseWrite()
	rd.Wait()
}

func TestMuxReadWrite(t *testing.T) {
	s, c, mux := channelPair(t)
	defer s.Close()
	defer c.Close()
	defer mux.Close()

	magic := "hello world"
	magicExt := "hello stderr"
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := s.Write([]byte(magic))
		if err != nil {
			t.Errorf("Write: %v", err)
			return
		}
		_, err = s.Extended(1).Write([]byte(magicExt))
		if err != nil {
			t.Errorf("Write: %v", err)
			return
		}
	}()

	var buf [1024]byte
	n, err := c.Read(buf[:])
	if err != nil {
		t.Fatalf("server Read: %v", err)
	}
	got := string(buf[:n])
	if got != magic {
		t.Fatalf("server: got %q want %q", got, magic)
	}

	n, err = c.Extended(1).Read(buf[:])
	if err != nil {
		t.Fatalf("server Read: %v", err)
	}

	got = string(buf[:n])
	if got != magicExt {
		t.Fatalf("server: got %q want %q", got, magic)
	}
}

func TestMuxChannelOverflow(t *testing.T) {
	reader, writer, mux := channelPair(t)
	defer reader.Close()
	defer writer.Close()
	defer mux.Close()

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := writer.Write(make([]byte, channelWindowSize)); err != nil {
			t.Errorf("could not fill window: %v", err)
		}
		writer.Write(make([]byte, 1))
	}()
	writer.remoteWin.waitWriterBlocked()

	// Send 1 byte.
	packet := make([]byte, 1+4+4+1)
	packet[0] = msgChannelData
	marshalUint32(packet[1:], writer.remoteId)
	marshalUint32(packet[5:], uint32(1))
	packet[9] = 42

	if err := writer.mux.conn.writePacket(packet); err != nil {
		t.Errorf("could not send packet")
	}
	if _, err := reader.SendRequest("hello", true, nil); err == nil {
		t.Errorf("SendRequest succeeded.")
	}
}

func TestMuxChannelReadUnblock(t *testing.T) {
	reader, writer, mux := channelPair(t)
	defer reader.Close()
	defer writer.Close()
	defer mux.Close()

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := writer.Write(make([]byte, channelWindowSize)); err != nil {
			t.Errorf("could not fill window: %v", err)
		}
		if _, err := writer.Write(make([]byte, 1)); err != nil {
			t.Errorf("Write: %v", err)
		}
		writer.Close()
	}()

	writer.remoteWin.waitWriterBlocked()

	buf := make([]byte, 32768)
	for {
		_, err := reader.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
}

func TestMuxChannelCloseWriteUnblock(t *testing.T) {
	reader, writer, mux := channelPair(t)
	defer reader.Close()
	defer writer.Close()
	defer mux.Close()

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := writer.Write(make([]byte, channelWindowSize)); err != nil {
			t.Errorf("could not fill window: %v", err)
		}
		if _, err := writer.Write(make([]byte, 1)); err != io.EOF {
			t.Errorf("got %v, want EOF for unblock write", err)
		}
	}()

	writer.remoteWin.waitWriterBlocked()
	reader.Close()
}

func TestMuxConnectionCloseWriteUnblock(t *testing.T) {
	reader, writer, mux := channelPair(t)
	defer reader.Close()
	defer writer.Close()
	defer mux.Close()

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := writer.Write(make([]byte, channelWindowSize)); err != nil {
			t.Errorf("could not fill window: %v", err)
		}
		if _, err := writer.Write(make([]byte, 1)); err != io.EOF {
			t.Errorf("got %v, want EOF for unblock write", err)
		}
	}()

	writer.remoteWin.waitWriterBlocked()
	mux.Close()
}

func TestMuxReject(t *testing.T) {
	client, server := muxPair()
	defer server.Close()
	defer client.Close()

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		defer wg.Done()

		ch, ok := <-server.incomingChannels
		if !ok {
			t.Error("cannot accept channel")
			return
		}
		if ch.ChannelType() != "ch" || string(ch.ExtraData()) != "extra" {
			t.Errorf("unexpected channel: %q, %q", ch.ChannelType(), ch.ExtraData())
			ch.Reject(RejectionReason(UnknownChannelType), UnknownChannelType.String())
			return
		}
		ch.Reject(RejectionReason(42), "message")
	}()

	ch, err := client.openChannel("ch", []byte("extra"))
	if ch != nil {
		t.Fatal("openChannel not rejected")
	}

	ocf, ok := err.(*OpenChannelError)
	if !ok {
		t.Errorf("got %#v want *OpenChannelError", err)
	} else if ocf.Reason != 42 || ocf.Message != "message" {
		t.Errorf("got %#v, want {Reason: 42, Message: %q}", ocf, "message")
	}

	want := "ssh: rejected: unknown reason 42 (message)"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestMuxChannelRequest(t *testing.T) {
	client, server, mux := channelPair(t)
	defer server.Close()
	defer client.Close()
	defer mux.Close()

	var received int
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		for r := range server.incomingRequests {
			received++
			r.Reply(r.Type == "yes", nil)
		}
		wg.Done()
	}()
	_, err := client.SendRequest("yes", false, nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	ok, err := client.SendRequest("yes", true, nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}

	if !ok {
		t.Errorf("SendRequest(yes): %v", ok)

	}

	ok, err = client.SendRequest("no", true, nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if ok {
		t.Errorf("SendRequest(no): %v", ok)
	}

	client.Close()
	wg.Wait()

	if received != 3 {
		t.Errorf("got %d requests, want %d", received, 3)
	}
}

func TestMuxUnknownChannelRequests(t *testing.T) {
	clientPipe, serverPipe := memPipe()
	client := newMux(clientPipe)
	defer serverPipe.Close()
	defer client.Close()

	kDone := make(chan error, 1)
	go func() {
		// Ignore unknown channel messages that don't want a reply.
		err := serverPipe.writePacket(Marshal(channelRequestMsg{
			PeersID:             1,
			Request:             "keepalive@openssh.com",
			WantReply:           false,
			RequestSpecificData: []byte{},
		}))
		if err != nil {
			kDone <- fmt.Errorf("send: %w", err)
			return
		}

		// Send a keepalive, which should get a channel failure message
		// in response.
		err = serverPipe.writePacket(Marshal(channelRequestMsg{
			PeersID:             2,
			Request:             "keepalive@openssh.com",
			WantReply:           true,
			RequestSpecificData: []byte{},
		}))
		if err != nil {
			kDone <- fmt.Errorf("send: %w", err)
			return
		}

		packet, err := serverPipe.readPacket()
		if err != nil {
			kDone <- fmt.Errorf("read packet: %w", err)
			return
		}
		decoded, err := decode(packet)
		if err != nil {
			kDone <- fmt.Errorf("decode failed: %w", err)
			return
		}

		switch msg := decoded.(type) {
		case *channelRequestFailureMsg:
			if msg.PeersID != 2 {
				kDone <- fmt.Errorf("received response to wrong message: %v", msg)
				return

			}
		default:
			kDone <- fmt.Errorf("unexpected channel message: %v", msg)
			return
		}

		kDone <- nil

		// Receive and respond to the keepalive to confirm the mux is
		// still processing requests.
		packet, err = serverPipe.readPacket()
		if err != nil {
			kDone <- fmt.Errorf("read packet: %w", err)
			return
		}
		if packet[0] != msgGlobalRequest {
			kDone <- errors.New("expected global request")
			return
		}

		err = serverPipe.writePacket(Marshal(globalRequestFailureMsg{
			Data: []byte{},
		}))
		if err != nil {
			kDone <- fmt.Errorf("failed to send failure msg: %w", err)
			return
		}

		close(kDone)
	}()

	// Wait for the server to send the keepalive message and receive back a
	// response.
	if err := <-kDone; err != nil {
		t.Fatal(err)
	}

	// Confirm client hasn't closed.
	if _, _, err := client.SendRequest("keepalive@golang.org", true, nil); err != nil {
		t.Fatalf("failed to send keepalive: %v", err)
	}

	// Wait for the server to shut down.
	if err := <-kDone; err != nil {
		t.Fatal(err)
	}
}

func TestMuxClosedChannel(t *testing.T) {
	clientPipe, serverPipe := memPipe()
	client := newMux(clientPipe)
	defer serverPipe.Close()
	defer client.Close()

	kDone := make(chan error, 1)
	go func() {
		// Open the channel.
		packet, err := serverPipe.readPacket()
		if err != nil {
			kDone <- fmt.Errorf("read packet: %w", err)
			return
		}
		if packet[0] != msgChannelOpen {
			kDone <- errors.New("expected chan open")
			return
		}

		var openMsg channelOpenMsg
		if err := Unmarshal(packet, &openMsg); err != nil {
			kDone <- fmt.Errorf("unmarshal: %w", err)
			return
		}

		// Send back the opened channel confirmation.
		err = serverPipe.writePacket(Marshal(channelOpenConfirmMsg{
			PeersID:       openMsg.PeersID,
			MyID:          0,
			MyWindow:      0,
			MaxPacketSize: channelMaxPacket,
		}))
		if err != nil {
			kDone <- fmt.Errorf("send: %w", err)
			return
		}

		// Close the channel.
		err = serverPipe.writePacket(Marshal(channelCloseMsg{
			PeersID: openMsg.PeersID,
		}))
		if err != nil {
			kDone <- fmt.Errorf("send: %w", err)
			return
		}

		// Send a keepalive message on the channel we just closed.
		err = serverPipe.writePacket(Marshal(channelRequestMsg{
			PeersID:             openMsg.PeersID,
			Request:             "keepalive@openssh.com",
			WantReply:           true,
			RequestSpecificData: []byte{},
		}))
		if err != nil {
			kDone <- fmt.Errorf("send: %w", err)
			return
		}

		// Receive the channel closed response.
		packet, err = serverPipe.readPacket()
		if err != nil {
			kDone <- fmt.Errorf("read packet: %w", err)
			return
		}
		if packet[0] != msgChannelClose {
			kDone <- errors.New("expected channel close")
			return
		}

		// Receive the keepalive response failure.
		packet, err = serverPipe.readPacket()
		if err != nil {
			kDone <- fmt.Errorf("read packet: %w", err)
			return
		}
		if packet[0] != msgChannelFailure {
			kDone <- errors.New("expected channel failure")
			return
		}
		kDone <- nil

		// Receive and respond to the keepalive to confirm the mux is
		// still processing requests.
		packet, err = serverPipe.readPacket()
		if err != nil {
			kDone <- fmt.Errorf("read packet: %w", err)
			return
		}
		if packet[0] != msgGlobalRequest {
			kDone <- errors.New("expected global request")
			return
		}

		err = serverPipe.writePacket(Marshal(globalRequestFailureMsg{
			Data: []byte{},
		}))
		if err != nil {
			kDone <- fmt.Errorf("failed to send failure msg: %w", err)
			return
		}

		close(kDone)
	}()

	// Open a channel.
	ch, err := client.openChannel("chan", nil)
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	defer ch.Close()

	// Wait for the server to close the channel and send the keepalive.
	<-kDone

	// Make sure the channel closed.
	if _, ok := <-ch.incomingRequests; ok {
		t.Fatalf("channel not closed")
	}

	// Confirm client hasn't closed
	if _, _, err := client.SendRequest("keepalive@golang.org", true, nil); err != nil {
		t.Fatalf("failed to send keepalive: %v", err)
	}

	// Wait for the server to shut down.
	<-kDone
}

func TestMuxGlobalRequest(t *testing.T) {
	var sawPeek bool
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		if !sawPeek {
			t.Errorf("never saw 'peek' request")
		}
	}()

	clientMux, serverMux := muxPair()
	defer serverMux.Close()
	defer clientMux.Close()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for r := range serverMux.incomingRequests {
			sawPeek = sawPeek || r.Type == "peek"
			if r.WantReply {
				err := r.Reply(r.Type == "yes",
					append([]byte(r.Type), r.Payload...))
				if err != nil {
					t.Errorf("AckRequest: %v", err)
				}
			}
		}
	}()

	_, _, err := clientMux.SendRequest("peek", false, nil)
	if err != nil {
		t.Errorf("SendRequest: %v", err)
	}

	ok, data, err := clientMux.SendRequest("yes", true, []byte("a"))
	if !ok || string(data) != "yesa" || err != nil {
		t.Errorf("SendRequest(\"yes\", true, \"a\"): %v %v %v",
			ok, data, err)
	}
	if ok, data, err := clientMux.SendRequest("yes", true, []byte("a")); !ok || string(data) != "yesa" || err != nil {
		t.Errorf("SendRequest(\"yes\", true, \"a\"): %v %v %v",
			ok, data, err)
	}

	if ok, data, err := clientMux.SendRequest("no", true, []byte("a")); ok || string(data) != "noa" || err != nil {
		t.Errorf("SendRequest(\"no\", true, \"a\"): %v %v %v",
			ok, data, err)
	}
}

func TestMuxGlobalRequestUnblock(t *testing.T) {
	clientMux, serverMux := muxPair()
	defer serverMux.Close()
	defer clientMux.Close()

	result := make(chan error, 1)
	go func() {
		_, _, err := clientMux.SendRequest("hello", true, nil)
		result <- err
	}()

	<-serverMux.incomingRequests
	serverMux.conn.Close()
	err := <-result

	if err != io.EOF {
		t.Errorf("want EOF, got %v", io.EOF)
	}
}

func TestMuxChannelRequestUnblock(t *testing.T) {
	a, b, connB := channelPair(t)
	defer a.Close()
	defer b.Close()
	defer connB.Close()

	result := make(chan error, 1)
	go func() {
		_, err := a.SendRequest("hello", true, nil)
		result <- err
	}()

	<-b.incomingRequests
	connB.conn.Close()
	err := <-result

	if err != io.EOF {
		t.Errorf("want EOF, got %v", err)
	}
}

func TestMuxCloseChannel(t *testing.T) {
	r, w, mux := channelPair(t)
	defer mux.Close()
	defer r.Close()
	defer w.Close()

	result := make(chan error, 1)
	go func() {
		var b [1024]byte
		_, err := r.Read(b[:])
		result <- err
	}()
	if err := w.Close(); err != nil {
		t.Errorf("w.Close: %v", err)
	}

	if _, err := w.Write([]byte("hello")); err != io.EOF {
		t.Errorf("got err %v, want io.EOF after Close", err)
	}

	if err := <-result; err != io.EOF {
		t.Errorf("got %v (%T), want io.EOF", err, err)
	}
}

func TestMuxCloseWriteChannel(t *testing.T) {
	r, w, mux := channelPair(t)
	defer mux.Close()

	result := make(chan error, 1)
	go func() {
		var b [1024]byte
		_, err := r.Read(b[:])
		result <- err
	}()
	if err := w.CloseWrite(); err != nil {
		t.Errorf("w.CloseWrite: %v", err)
	}

	if _, err := w.Write([]byte("hello")); err != io.EOF {
		t.Errorf("got err %v, want io.EOF after CloseWrite", err)
	}

	if err := <-result; err != io.EOF {
		t.Errorf("got %v (%T), want io.EOF", err, err)
	}
}

func TestMuxInvalidRecord(t *testing.T) {
	a, b := muxPair()
	defer a.Close()
	defer b.Close()

	packet := make([]byte, 1+4+4+1)
	packet[0] = msgChannelData
	marshalUint32(packet[1:], 29348723 /* invalid channel id */)
	marshalUint32(packet[5:], 1)
	packet[9] = 42

	a.conn.writePacket(packet)
	go a.SendRequest("hello", false, nil)
	// 'a' wrote an invalid packet, so 'b' has exited.
	req, ok := <-b.incomingRequests
	if ok {
		t.Errorf("got request %#v after receiving invalid packet", req)
	}
}

func TestZeroWindowAdjust(t *testing.T) {
	a, b, mux := channelPair(t)
	defer a.Close()
	defer b.Close()
	defer mux.Close()

	go func() {
		io.WriteString(a, "hello")
		// bogus adjust.
		a.sendMessage(windowAdjustMsg{})
		io.WriteString(a, "world")
		a.Close()
	}()

	want := "helloworld"
	c, _ := io.ReadAll(b)
	if string(c) != want {
		t.Errorf("got %q want %q", c, want)
	}
}

func TestMuxMaxPacketSize(t *testing.T) {
	a, b, mux := channelPair(t)
	defer a.Close()
	defer b.Close()
	defer mux.Close()

	large := make([]byte, a.maxRemotePayload+1)
	packet := make([]byte, 1+4+4+1+len(large))
	packet[0] = msgChannelData
	marshalUint32(packet[1:], a.remoteId)
	marshalUint32(packet[5:], uint32(len(large)))
	packet[9] = 42

	if err := a.mux.conn.writePacket(packet); err != nil {
		t.Errorf("could not send packet")
	}

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		a.SendRequest("hello", false, nil)
		wg.Done()
	}()

	_, ok := <-b.incomingRequests
	if ok {
		t.Errorf("connection still alive after receiving large packet.")
	}
}

func TestMuxChannelWindowDeferredUpdates(t *testing.T) {
	s, c, mux := channelPair(t)
	cTransport := mux.conn.(*memTransport)
	defer s.Close()
	defer c.Close()
	defer mux.Close()

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)

	data := make([]byte, 1024)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := s.Write(data)
		if err != nil {
			t.Errorf("Write: %v", err)
			return
		}
	}()
	cWritesInit := cTransport.getWriteCount()
	buf := make([]byte, 1)
	for i := 0; i < len(data); i++ {
		n, err := c.Read(buf)
		if n != len(buf) || err != nil {
			t.Fatalf("Read: %v, %v", n, err)
		}
	}
	cWrites := cTransport.getWriteCount() - cWritesInit
	// reading 1 KiB should not cause any window updates to be sent, but allow
	// for some unexpected writes
	if cWrites > 30 {
		t.Fatalf("reading 1 KiB from channel caused %v writes", cWrites)
	}
}

func TestMuxChannelRejectRemovesFromMux(t *testing.T) {
	serverMux, clientMux := muxPair()
	defer serverMux.Close()
	defer clientMux.Close()

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)

	go func() {
		defer wg.Done()

		// The server waits for the channel creation request
		newCh, ok := <-serverMux.incomingChannels
		if !ok {
			t.Error("failed to accept channel")
			return
		}
		ch := newCh.(*channel)

		if serverMux.chanList.getChan(ch.localId) == nil {
			t.Errorf("channel %d is not in the chanList before Reject", ch.localId)
		}

		if err := ch.Reject(Prohibited, "rejecting this channel"); err != nil {
			t.Errorf("Reject failed: %v", err)
		}

		if serverMux.chanList.getChan(ch.localId) != nil {
			t.Errorf("channel %d is still in the chanList after Reject", ch.localId)
		}
	}()

	_, _, err := clientMux.OpenChannel("test_leak", nil)

	if err == nil {
		t.Fatal("expected an error (channel rejected), but got nil")
	}

	if _, ok := err.(*OpenChannelError); !ok {
		t.Errorf("expected *OpenChannelError, got: %T", err)
	}
}

// Don't ship code with debug=true.
func TestDebug(t *testing.T) {
	if debugMux {
		t.Error("mux debug switched on")
	}
	if debugHandshake {
		t.Error("handshake debug switched on")
	}
	if debugTransport {
		t.Error("transport debug switched on")
	}
}

func TestMuxUnexpectedGlobalResponsesDiscarded(t *testing.T) {
	clientPipe, serverPipe := memPipe()
	client := newMux(clientPipe)
	defer serverPipe.Close()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		// Send multiple unexpected global responses, this should not block the
		// globalResponses channel.
		for i := range 5 {
			err := serverPipe.writePacket(Marshal(globalRequestSuccessMsg{
				Data: []byte{byte(i)},
			}))
			if err != nil {
				done <- fmt.Errorf("send success msg %d: %w", i, err)
				return
			}
		}
		for i := range 5 {
			err := serverPipe.writePacket(Marshal(globalRequestFailureMsg{
				Data: []byte{byte(i)},
			}))
			if err != nil {
				done <- fmt.Errorf("send failure msg %d: %w", i, err)
				return
			}
		}

		// Now send a global request and wait for the response. This
		// verifies the mux is still processing packets.
		err := serverPipe.writePacket(Marshal(globalRequestMsg{
			Type:      "keepalive@golang.org",
			WantReply: true,
			Data:      nil,
		}))
		if err != nil {
			done <- fmt.Errorf("send global request: %w", err)
			return
		}

		packet, err := serverPipe.readPacket()
		if err != nil {
			done <- fmt.Errorf("read packet: %w", err)
			return
		}
		decoded, err := decode(packet)
		if err != nil {
			done <- fmt.Errorf("decode: %w", err)
			return
		}
		switch decoded.(type) {
		case *globalRequestSuccessMsg, *globalRequestFailureMsg:
			// Expected response
		default:
			done <- fmt.Errorf("unexpected packet type: %T", decoded)
			return
		}
		done <- nil
	}()

	// Handle the incoming request from the server and reply
	req, ok := <-client.incomingRequests
	if !ok {
		t.Fatal("incomingRequests channel closed unexpectedly")
	}
	if req.Type != "keepalive@golang.org" {
		t.Fatalf("unexpected request type: %s", req.Type)
	}
	if err := req.Reply(true, nil); err != nil {
		t.Fatalf("Reply: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMuxConcurrentGlobalRequests(t *testing.T) {
	clientMux, serverMux := muxPair()
	defer serverMux.Close()
	defer clientMux.Close()

	const numRequests = 50

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for r := range serverMux.incomingRequests {
			if r.WantReply {
				replyData := append([]byte("reply:"), r.Payload...)
				r.Reply(true, replyData)
			}
		}
	}()

	var clientWg sync.WaitGroup
	clientWg.Add(numRequests)

	errCh := make(chan error, numRequests)

	for i := range numRequests {
		go func(id int) {
			defer clientWg.Done()

			payloadStr := fmt.Sprintf("req-%d", id)
			payload := []byte(payloadStr)

			// This call blocks until the globalSentMu is acquired.
			// The mutex ensures that even with many concurrent attempts,
			// the "drain" and "send" logic happens atomically per request.
			ok, data, err := clientMux.SendRequest("echo", true, payload)
			if err != nil {
				errCh <- fmt.Errorf("req %d error: %v", id, err)
				return
			}
			if !ok {
				errCh <- fmt.Errorf("req %d failed (want success)", id)
				return
			}

			expected := "reply:" + payloadStr
			if string(data) != expected {
				errCh <- fmt.Errorf("req %d mismatch: got %q, want %q", id, string(data), expected)
			}
		}(i)
	}

	clientWg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	clientMux.Close()
	<-serverDone
}

func TestMuxGlobalResponseDroppedWhenIdle(t *testing.T) {
	clientPipe, serverPipe := memPipe()
	clientMux := newMux(clientPipe)
	defer serverPipe.Close()
	defer clientMux.Close()

	errCh := make(chan error, 1)
	go func() {
		// Send a spurious response while no SendRequest is pending.
		if err := serverPipe.writePacket(Marshal(globalRequestSuccessMsg{
			Data: []byte("spurious"),
		})); err != nil {
			errCh <- fmt.Errorf("send spurious: %w", err)
			return
		}
		// Follow with a global request; once the client observes this on
		// incomingRequests, the mux loop has necessarily processed (and
		// dropped) the prior spurious response.
		if err := serverPipe.writePacket(Marshal(globalRequestMsg{
			Type:      "sync@example.com",
			WantReply: false,
		})); err != nil {
			errCh <- fmt.Errorf("send sync request: %w", err)
			return
		}
		errCh <- nil
	}()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	req, ok := <-clientMux.incomingRequests
	if !ok {
		t.Fatal("incomingRequests closed unexpectedly")
	}
	if req.Type != "sync@example.com" {
		t.Fatalf("unexpected sync request type %q", req.Type)
	}

	// The spurious response preceded the sync request, so by now the mux
	// loop has processed it. The pending-gate must have caused it to be
	// dropped rather than buffered.
	if n := len(clientMux.globalResponses); n != 0 {
		t.Fatalf("globalResponses buffer should be empty after idle drop, has %d entries", n)
	}
}

func TestMuxStaleResponseDrained(t *testing.T) {
	// Simulate a stale response sitting in globalResponses (e.g. a response
	// that slipped in through the pending-gate on a prior SendRequest that
	// exited without consuming it). The drain step in the next SendRequest
	// must discard it so the caller receives the correct reply.
	clientMux, serverMux := muxPair()
	defer serverMux.Close()
	defer clientMux.Close()

	clientMux.globalResponses <- &globalRequestSuccessMsg{Data: []byte("stale")}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for req := range serverMux.incomingRequests {
			if req.WantReply {
				req.Reply(true, append([]byte("reply:"), req.Payload...))
			}
		}
	}()

	ok, data, err := clientMux.SendRequest("test", true, []byte("hello"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if !ok {
		t.Fatal("expected success response")
	}
	if string(data) != "reply:hello" {
		t.Fatalf("got %q, want %q (drain did not remove stale response)", data, "reply:hello")
	}

	clientMux.Close()
	<-serverDone
}

func TestMuxGlobalResponseAcceptedWhilePending(t *testing.T) {
	// Positive control: when a SendRequest is actually pending, the
	// response must be delivered (the gate is open).
	clientMux, serverMux := muxPair()
	defer serverMux.Close()
	defer clientMux.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for req := range serverMux.incomingRequests {
			if req.WantReply {
				req.Reply(true, []byte("pong"))
			}
		}
	}()

	ok, data, err := clientMux.SendRequest("ping", true, nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if !ok || string(data) != "pong" {
		t.Fatalf("unexpected response: ok=%v data=%q", ok, data)
	}

	clientMux.Close()
	<-serverDone
}

func TestChannelUnexpectedResponsesDiscarded(t *testing.T) {
	// A malicious peer that spams channelRequestSuccess/Failure messages
	// for an open, idle channel must not be able to stall the mux read
	// loop by filling ch.msg. After the flood, the channel must still be
	// usable: a subsequent legitimate SendRequest receives its reply.
	clientMux, serverMux := muxPair()
	defer serverMux.Close()
	defer clientMux.Close()

	serverRes := make(chan *channel, 1)
	go func() {
		newCh, ok := <-serverMux.incomingChannels
		if !ok {
			close(serverRes)
			return
		}
		c, _, err := newCh.Accept()
		if err != nil {
			close(serverRes)
			return
		}
		serverRes <- c.(*channel)
	}()

	clientCh, err := clientMux.openChannel("chan", nil)
	if err != nil {
		t.Fatalf("openChannel: %v", err)
	}
	serverCh := <-serverRes
	if serverCh == nil {
		t.Fatal("server did not accept channel")
	}

	// Spam many unsolicited success/failure responses. More than chanSize
	// to ensure ch.msg would overflow without the pending-gate.
	const spam = chanSize * 4
	done := make(chan error, 1)
	go func() {
		for i := range spam {
			if err := serverCh.ackRequest(i%2 == 0); err != nil {
				done <- fmt.Errorf("ackRequest %d: %w", i, err)
				return
			}
		}
		// Echo any legitimate request back.
		for req := range serverCh.incomingRequests {
			if req.WantReply {
				if err := req.Reply(true, append([]byte("reply:"), req.Payload...)); err != nil {
					done <- fmt.Errorf("reply: %w", err)
					return
				}
			}
		}
		done <- nil
	}()

	// If the flood had wedged the mux loop, this SendRequest would never
	// receive a reply.
	ok, err := clientCh.SendRequest("ping", true, []byte("hello"))
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if !ok {
		t.Fatal("expected success reply")
	}

	// Clean up so the server goroutine can exit.
	clientCh.Close()
	serverCh.Close()
	if err := <-done; err != nil {
		if !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	}
}

func TestChannelConcurrentRequests(t *testing.T) {
	writer, reader, mux := channelPair(t)
	defer writer.Close()
	defer reader.Close()
	defer mux.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for req := range writer.incomingRequests {
			if req.WantReply {
				req.Reply(true, append([]byte("reply:"), req.Payload...))
			}
		}
	}()

	const numRequests = 50
	var wg sync.WaitGroup
	wg.Add(numRequests)
	errCh := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer wg.Done()
			payload := []byte(fmt.Sprintf("req-%d", id))
			ok, err := reader.SendRequest("echo", true, payload)
			if err != nil {
				errCh <- fmt.Errorf("req %d: %v", id, err)
				return
			}
			if !ok {
				errCh <- fmt.Errorf("req %d: expected success", id)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	reader.Close()
	writer.Close()
	<-serverDone
}

func TestChannelResponseDroppedWhenIdle(t *testing.T) {
	// A spurious response arriving while no SendRequest is pending must
	// be dropped rather than buffered in ch.msg.
	writer, reader, mux := channelPair(t)
	defer writer.Close()
	defer reader.Close()
	defer mux.Close()

	// Server sends an unsolicited reply, then a request so we can
	// synchronise: once the client observes the request, the mux loop has
	// necessarily processed (and dropped) the prior spurious reply.
	errCh := make(chan error, 1)
	go func() {
		if err := writer.ackRequest(true); err != nil {
			errCh <- err
			return
		}
		if _, err := writer.SendRequest("sync", false, nil); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	req := <-reader.incomingRequests
	if req.Type != "sync" {
		t.Fatalf("unexpected request type %q", req.Type)
	}

	if n := len(reader.msg); n != 0 {
		t.Fatalf("ch.msg should be empty after idle drop, has %d entries", n)
	}

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestChannelStaleResponseDrained(t *testing.T) {
	// Simulate a stale response sitting in ch.msg (e.g. a response that
	// slipped through the pending-gate on a prior SendRequest that exited
	// without consuming it). The drain step in the next SendRequest must
	// discard it so the caller receives the correct reply.
	writer, reader, mux := channelPair(t)
	defer writer.Close()
	defer reader.Close()
	defer mux.Close()

	reader.msg <- &channelRequestSuccessMsg{PeersID: reader.remoteId}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for req := range writer.incomingRequests {
			if req.WantReply {
				req.Reply(false, append([]byte("nack:"), req.Payload...))
			}
		}
	}()

	ok, err := reader.SendRequest("test", true, []byte("hello"))
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	// If the stale success had been consumed, ok would be true.
	if ok {
		t.Fatal("got stale success response; drain did not remove it")
	}

	reader.Close()
	writer.Close()
	<-serverDone
}

func TestChannelResponseAcceptedWhilePending(t *testing.T) {
	// Positive control: when a SendRequest is actually pending, the
	// response must be delivered (the gate is open).
	writer, reader, mux := channelPair(t)
	defer writer.Close()
	defer reader.Close()
	defer mux.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for req := range writer.incomingRequests {
			if req.WantReply {
				req.Reply(true, nil)
			}
		}
	}()

	ok, err := reader.SendRequest("ping", true, nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if !ok {
		t.Fatal("expected success")
	}

	reader.Close()
	writer.Close()
	<-serverDone
}

func TestChannelSendRequestAfterCloseNoSpinloop(t *testing.T) {
	writer, reader, mux := channelPair(t)
	defer writer.Close()
	defer mux.Close()

	// Close the channel, this closes reader.msg.
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Wait for reader.msg to actually be closed by the mux loop.
	for {
		select {
		case _, ok := <-reader.msg:
			if !ok {
				goto closed
			}
		case <-time.After(2 * time.Second):
			t.Fatal("reader.msg was not closed")
		}
	}
closed:

	done := make(chan error, 1)
	go func() {
		_, err := reader.SendRequest("ping", true, nil)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("SendRequest after close: got %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendRequest spinloop on closed ch.msg")
	}
}

func TestMuxSendRequestAfterCloseNoSpinloop(t *testing.T) {
	a, b := muxPair()
	defer a.Close()

	// Closing the underlying conn causes mux.loop to exit and close
	// b.globalResponses.
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Wait for globalResponses to actually be closed by loop().
	for {
		select {
		case _, ok := <-b.globalResponses:
			if !ok {
				goto closed
			}
		case <-time.After(2 * time.Second):
			t.Fatal("globalResponses was not closed")
		}
	}
closed:

	done := make(chan error, 1)
	go func() {
		_, _, err := b.SendRequest("ping", true, nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("SendRequest after close: got nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendRequest spinloop on closed globalResponses")
	}
}
