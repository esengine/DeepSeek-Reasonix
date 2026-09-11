package provider

import "testing"

func TestClassifyImage(t *testing.T) {
	cases := []struct {
		in   string
		want ImageKind
	}{
		{"data:image/png;base64,AQIDBA==", ImageDataURL},
		{"https://cdn.example.com/cat.png", ImageHTTPURL},
		{"https://cdn.example.com/cat.png?w=800", ImageHTTPURL},
		{"http://cdn.example.com/shot.WEBP", ImageHTTPURL},
		{"file-api-0a1b2c3d4e5f60718293a4b5c6d7e8f9", ImageFileID},
		{"https://example.com/readme.md", ImageNone},
		{"https://user:pass@cdn.example.com/cat.png", ImageNone},
		{"file-api-", ImageNone},
		{"", ImageNone},
	}
	for _, tc := range cases {
		if got := ClassifyImage(tc.in); got != tc.want {
			t.Errorf("ClassifyImage(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseImageDataURL(t *testing.T) {
	mt, data, ok := ParseImageDataURL("data:image/png;base64,AQIDBA==")
	if !ok || mt != "image/png" || data != "AQIDBA==" {
		t.Fatalf("got (%q, %q, %v), want (image/png, AQIDBA==, true)", mt, data, ok)
	}
	for _, bad := range []string{
		"",
		"data:image/png,AQID",      // not base64-encoded
		"http://example.com/x.png", // not a data URL
		"data:image/png;base64",    // no payload separator
		"data:;base64,AAAA",        // empty media type
	} {
		if _, _, ok := ParseImageDataURL(bad); ok {
			t.Errorf("ParseImageDataURL(%q) = ok, want not-ok", bad)
		}
	}
}
