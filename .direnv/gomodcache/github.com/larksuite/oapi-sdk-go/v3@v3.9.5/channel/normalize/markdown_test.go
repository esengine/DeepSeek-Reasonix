package normalize

import (
	"fmt"
	"testing"
)

func TestSimpleMarkdownToPost(t *testing.T) {
	md := "Hello World\nThis is a [link](https://larksuite.com)"
	post, err := SimpleMarkdownToPost("Title", md, nil)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(post)
}
