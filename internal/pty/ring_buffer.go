package pty

import (
	"bytes"
	"sync"
)

// DefaultBufferSize is the default capacity for PTY session output history (256 KiB).
const DefaultBufferSize = 256 * 1024

// RingBuffer is a thread-safe bounded circular byte buffer for terminal output.
type RingBuffer struct {
	mu       sync.RWMutex
	buf      []byte
	size     int
	capacity int
	head     int // Next write position
	tail     int // Oldest data position
	cursor   int // Unread read position (relative to tail)
}

// NewRingBuffer allocates a RingBuffer with the given maximum capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultBufferSize
	}
	return &RingBuffer{
		buf:      make([]byte, capacity),
		capacity: capacity,
	}
}

// Write appends bytes to the ring buffer, overwriting the oldest bytes if full.
func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for _, b := range p {
		rb.buf[rb.head] = b
		rb.head = (rb.head + 1) % rb.capacity
		if rb.size < rb.capacity {
			rb.size++
		} else {
			// Buffer is full; advancing tail drops the oldest byte
			rb.tail = (rb.tail + 1) % rb.capacity
			if rb.cursor > 0 {
				rb.cursor--
			}
		}
	}
	return len(p), nil
}

// ReadUnread reads all bytes written since the last ReadUnread call, capped by maxBytes.
func (rb *RingBuffer) ReadUnread(maxBytes int) []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	unreadCount := rb.size - rb.cursor
	if unreadCount <= 0 {
		return nil
	}
	if maxBytes > 0 && unreadCount > maxBytes {
		unreadCount = maxBytes
	}

	result := make([]byte, unreadCount)
	startPos := (rb.tail + rb.cursor) % rb.capacity
	for i := 0; i < unreadCount; i++ {
		result[i] = rb.buf[(startPos+i)%rb.capacity]
	}
	rb.cursor += unreadCount
	return result
}

// ReadTail reads up to maxBytes of the most recent output without advancing the unread cursor.
func (rb *RingBuffer) ReadTail(maxBytes int) []byte {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return nil
	}
	count := rb.size
	if maxBytes > 0 && count > maxBytes {
		count = maxBytes
	}

	result := make([]byte, count)
	startPos := (rb.head - count + rb.capacity) % rb.capacity
	for i := 0; i < count; i++ {
		result[i] = rb.buf[(startPos+i)%rb.capacity]
	}
	return result
}

// Bytes returns a full linear copy of all buffered data from oldest to newest.
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return nil
	}
	result := make([]byte, rb.size)
	for i := 0; i < rb.size; i++ {
		result[i] = rb.buf[(rb.tail+i)%rb.capacity]
	}
	return result
}

// Len returns the number of bytes currently stored in the buffer.
func (rb *RingBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// Reset clears the buffer content and resets all pointers.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.tail = 0
	rb.size = 0
	rb.cursor = 0
	rb.buf = bytes.Repeat([]byte{0}, rb.capacity)
}
