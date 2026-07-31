package mimetype_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/wailsapp/mimetype"
)

// Pure io.Readers (meaning those without a Seek method) cannot be read twice.
// This means that once DetectReader has been called on an io.Reader, that reader
// is missing the bytes representing the header of the file.
// To detect the MIME type and then reuse the input, use a buffer, io.TeeReader,
// and io.MultiReader to create a new reader containing the original, unaltered data.
//
// If the input is an io.ReadSeeker instead, call input.Seek(0, io.SeekStart)
// before reusing it.
func Example_detectReader() {
	testBytes := []byte("This random text has a MIME type of text/plain; charset=utf-8.")
	input := bytes.NewReader(testBytes)

	mtype, recycledInput, err := recycleReader(input)

	// Verify recycledInput contains the original input.
	text, _ := ioutil.ReadAll(recycledInput)
	fmt.Println(mtype, bytes.Equal(testBytes, text), err)
	// Output: text/plain; charset=utf-8 true <nil>
}

// recycleReader returns the MIME type of input and a new reader
// containing the whole data from input.
func recycleReader(input io.Reader) (mimeType string, recycled io.Reader, err error) {
	// header will store the bytes mimetype uses for detection.
	header := bytes.NewBuffer(nil)

	// After DetectReader, the data read from input is copied into header.
	mtype, err := mimetype.DetectReader(io.TeeReader(input, header))

	// Concatenate back the header to the rest of the file.
	// recycled now contains the complete, original data.
	recycled = io.MultiReader(header, input)

	return mtype.String(), recycled, err
}
