package provider

import "testing"

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
