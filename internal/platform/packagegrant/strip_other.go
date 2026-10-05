//go:build !windows

package packagegrant

// Strip changes nothing here: only Windows sandboxes read an app package from a
// file's access entries.
func Strip(string) (Report, error) {
	return Report{Stripped: []string{}, Refused: []Failure{}, Unread: []Failure{}}, nil
}
