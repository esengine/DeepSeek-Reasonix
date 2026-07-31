// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"testing"
	"testing/synctest"
)

func TestSkipPackets(t *testing.T) {
	synctest.Test(t, testSkipPackets)
}
func testSkipPackets(t *testing.T) {
	tc, s := newTestConnAndLocalStream(t, serverSide, uniStream, permissiveTransportParameters)
	connWritesPacket := func() {
		s.WriteByte(0)
		s.Flush()
		tc.wantFrameType("conn sends STREAM data",
			packetType1RTT, debugFrameStream{})
		tc.writeAckForLatest()
		tc.wantIdle("conn is idle")
	}
	connWritesPacket()

expectSkip:
	for maxUntilSkip := 256; maxUntilSkip <= 1024; maxUntilSkip *= 2 {
		for range maxUntilSkip + 1 {
			nextNum := tc.lastPacket.num + 1

			connWritesPacket()

			if tc.lastPacket.num == nextNum+1 {
				// A packet number was skipped, as expected.
				continue expectSkip
			}
			if tc.lastPacket.num != nextNum {
				t.Fatalf("got packet number %v, want %v or %v+1", tc.lastPacket.num, nextNum, nextNum)
			}

		}
		t.Fatalf("no numbers skipped after %v packets", maxUntilSkip)
	}
}

func TestSkipAckForSkippedPacket(t *testing.T) {
	synctest.Test(t, testSkipAckForSkippedPacket)
}
func testSkipAckForSkippedPacket(t *testing.T) {
	tc, s := newTestConnAndLocalStream(t, serverSide, uniStream, permissiveTransportParameters)

	// Cause the connection to send packets until it skips a packet number.
	for {
		// Cause the connection to send a packet.
		last := tc.lastPacket
		s.WriteByte(0)
		s.Flush()
		tc.wantFrameType("conn sends STREAM data",
			packetType1RTT, debugFrameStream{})

		if tc.lastPacket.num > 256 {
			t.Fatalf("no numbers skipped after 256 packets")
		}

		// Acknowledge everything up to the packet before the one we just received.
		// We don't acknowledge the most-recently-received packet, because doing
		// so will cause the connection to drop state for the skipped packet number.
		// (We only retain state up to the oldest in-flight packet.)
		//
		// If the conn has skipped a packet number, then this ack will improperly
		// acknowledge the unsent packet.
		t.Log(tc.lastPacket.num)
		tc.writeFrames(tc.lastPacket.ptype, debugFrameAck{
			ranges: []i64range[packetNumber]{{0, tc.lastPacket.num}},
		})

		if last != nil && tc.lastPacket.num == last.num+2 {
			// The connection has skipped a packet number.
			break
		}
	}

	// We wrote an ACK for a skipped packet number.
	// The connection should close.
	tc.wantFrame("ACK for skipped packet causes CONNECTION_CLOSE",
		packetType1RTT, debugFrameConnectionCloseTransport{
			code: errProtocolViolation,
		})
}
