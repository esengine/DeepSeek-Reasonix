package pe

import (
	"encoding/binary"
	"reflect"
	"testing"
)

// TestSizeValues ensures the size constants are correct.
func TestSizeValues(t *testing.T) {
	tests := []struct {
		Struct   interface{}
		Expected int
	}{
		{ImageDOSHeader{}, SizeOfImageDOSHeader},
		{ImageFileHeader{}, SizeOfImageFileHeader},
		{ImageOptionalHeader32{}, SizeOfImageOptionalHeader32},
		{ImageOptionalHeader64{}, SizeOfImageOptionalHeader64},
		{ImageNTHeaders32{}, SizeOfImageNTHeaders32},
		{ImageNTHeaders64{}, SizeOfImageNTHeaders64},
		{ImageDataDirectory{}, SizeOfImageDataDirectory},
	}

	for _, test := range tests {
		actual := binary.Size(test.Struct)
		if actual != test.Expected {
			t.Errorf("Size of failed for %s: expected %d, got %d (Δ %d)", reflect.TypeOf(test.Struct).Name(), test.Expected, actual, test.Expected-actual)
		}
	}
}
