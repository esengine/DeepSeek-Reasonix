package random

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test(t *testing.T) {
	assert.Len(t, String(32), 32)
	r := New()
	assert.Regexp(t, regexp.MustCompile("[0-9]+$"), r.String(8, Numeric))
}

func TestRandomString(t *testing.T) {
	var testCases = []struct {
		name       string
		whenLength uint8
		expect     string
	}{
		{
			name:       "ok, 16",
			whenLength: 16,
		},
		{
			name:       "ok, 32",
			whenLength: 32,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uid := String(tc.whenLength, Alphabetic)
			assert.Len(t, uid, int(tc.whenLength))
		})
	}
}

func TestRandomStringBias(t *testing.T) {
	t.Parallel()
	const slen = 33
	const loop = 100000

	counts := make(map[rune]int)
	var count int64

	for i := 0; i < loop; i++ {
		s := String(slen, Alphabetic)
		require.Equal(t, slen, len(s))
		for _, b := range s {
			counts[b]++
			count++
		}
	}

	require.Equal(t, len(Alphabetic), len(counts))

	avg := float64(count) / float64(len(counts))
	for k, n := range counts {
		diff := float64(n) / avg
		if diff < 0.95 || diff > 1.05 {
			t.Errorf("Bias on '%c': expected average %f, got %d", k, avg, n)
		}
	}
}
