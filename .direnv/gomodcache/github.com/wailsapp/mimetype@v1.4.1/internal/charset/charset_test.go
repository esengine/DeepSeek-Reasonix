package charset

import (
	"testing"
)

const xmlDoc = `<?xml version="1.0" encoding="UTF-8"?>
<note>
  <to>Tove</to>
  <from>Jani</from>
  <heading>Reminder</heading>
  <body>Don't forget me this weekend!</body>
</note>`
const htmlDoc = `<!DOCTYPE html>
<html>
  <head><!--[if lt IE 9]><script language="javascript" type="text/javascript" src="//html5shim.googlecode.com/svn/trunk/html5.js"></script><![endif]-->
    <meta charset="UTF-8"><style>/*
     </style>
    <link rel="stylesheet" href="css/animation.css"><!--[if IE 7]><link rel="stylesheet" href="css/" + font.fontname + "-ie7.css"><![endif]-->
    <script>
    </script>
  </head>
  <body>
    <div class="container footer">さ</div>
  </body>
</html>`

func TestFromXML(t *testing.T) {
	charset := FromXML([]byte(xmlDoc))
	if charset != "utf-8" {
		t.Errorf("expected: utf-8; got: %s", charset)
	}
}

func TestFromHTML(t *testing.T) {
	charset := FromHTML([]byte(htmlDoc))
	if charset != "utf-8" {
		t.Errorf("expected: utf-8; got: %s", charset)
	}
}

func TestFromPlain(t *testing.T) {
	tcases := []struct {
		raw     []byte
		charset string
	}{
		{[]byte{0xe6, 0xf8, 0xe5, 0x85, 0x85}, "windows-1252"},
		{[]byte{0xe6, 0xf8, 0xe5}, "iso-8859-1"},
		{[]byte("æøå"), "utf-8"},
		{[]byte{}, ""},
	}
	for _, tc := range tcases {
		if cs := FromPlain(tc.raw); cs != tc.charset {
			t.Errorf("in: %v; expected: %s; got: %s", tc.raw, tc.charset, cs)
		}
	}
}
