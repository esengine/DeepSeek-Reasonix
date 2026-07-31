package tmp

// TestStruct ...
type TestStruct struct {
	_          int `bu:"ba"`
	Exported   string
	unexported int `aaa:"bbb" ccc:"ddd"`
}

func init() {
	var ts TestStruct
	_ = ts.unexported
}
