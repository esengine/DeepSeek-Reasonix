package version

import "time"

// Conversion of Windows Time Stamp

// Difference Unix epoch in Windows timestamp format.
const unixEpoch = 0x019DB1DED53E8000

// Windows timestamp is in "hecto-nanoseconds" (100 ns)
const tsUnitToNano = 100

func timeToTimestamp(t time.Time) (uint32, uint32) {
	if t.IsZero() {
		return 0, 0
	}
	ts := t.UnixNano()/tsUnitToNano + unixEpoch
	return uint32(uint64(ts) >> 32), uint32(ts)
}

func timestampToTime(ms uint32, ls uint32) time.Time {
	if ms == 0 && ls == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64((uint64(ms)<<32|uint64(ls))-unixEpoch)*tsUnitToNano)
}
