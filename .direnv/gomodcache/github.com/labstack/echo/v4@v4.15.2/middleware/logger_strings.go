// SPDX-License-Identifier: BSD-3-Clause
// SPDX-FileCopyrightText: Copyright 2010 The Go Authors
//
// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
//
// Go LICENSE https://raw.githubusercontent.com/golang/go/36bca3166e18db52687a4d91ead3f98ffe6d00b8/LICENSE
/**
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

package middleware

import (
	"bytes"
	"unicode/utf8"
)

// This function is modified copy from Go standard library encoding/json/encode.go `appendString` function
// Source: https://github.com/golang/go/blob/36bca3166e18db52687a4d91ead3f98ffe6d00b8/src/encoding/json/encode.go#L999
func writeJSONSafeString(buf *bytes.Buffer, src string) (int, error) {
	const hex = "0123456789abcdef"

	written := 0
	start := 0
	for i := 0; i < len(src); {
		if b := src[i]; b < utf8.RuneSelf {
			if safeSet[b] {
				i++
				continue
			}

			n, err := buf.Write([]byte(src[start:i]))
			written += n
			if err != nil {
				return written, err
			}
			switch b {
			case '\\', '"':
				n, err := buf.Write([]byte{'\\', b})
				written += n
				if err != nil {
					return written, err
				}
			case '\b':
				n, err := buf.Write([]byte{'\\', 'b'})
				written += n
				if err != nil {
					return n, err
				}
			case '\f':
				n, err := buf.Write([]byte{'\\', 'f'})
				written += n
				if err != nil {
					return written, err
				}
			case '\n':
				n, err := buf.Write([]byte{'\\', 'n'})
				written += n
				if err != nil {
					return written, err
				}
			case '\r':
				n, err := buf.Write([]byte{'\\', 'r'})
				written += n
				if err != nil {
					return written, err
				}
			case '\t':
				n, err := buf.Write([]byte{'\\', 't'})
				written += n
				if err != nil {
					return written, err
				}
			default:
				// This encodes bytes < 0x20 except for \b, \f, \n, \r and \t.
				n, err := buf.Write([]byte{'\\', 'u', '0', '0', hex[b>>4], hex[b&0xF]})
				written += n
				if err != nil {
					return written, err
				}
			}
			i++
			start = i
			continue
		}
		srcN := min(len(src)-i, utf8.UTFMax)
		c, size := utf8.DecodeRuneInString(src[i : i+srcN])
		if c == utf8.RuneError && size == 1 {
			n, err := buf.Write([]byte(src[start:i]))
			written += n
			if err != nil {
				return written, err
			}
			n, err = buf.Write([]byte(`\ufffd`))
			written += n
			if err != nil {
				return written, err
			}
			i += size
			start = i
			continue
		}
		i += size
	}
	n, err := buf.Write([]byte(src[start:]))
	written += n
	return written, err
}

// safeSet holds the value true if the ASCII character with the given array
// position can be represented inside a JSON string without any further
// escaping.
//
// All values are true except for the ASCII control characters (0-31), the
// double quote ("), and the backslash character ("\").
var safeSet = [utf8.RuneSelf]bool{
	' ':      true,
	'!':      true,
	'"':      false,
	'#':      true,
	'$':      true,
	'%':      true,
	'&':      true,
	'\'':     true,
	'(':      true,
	')':      true,
	'*':      true,
	'+':      true,
	',':      true,
	'-':      true,
	'.':      true,
	'/':      true,
	'0':      true,
	'1':      true,
	'2':      true,
	'3':      true,
	'4':      true,
	'5':      true,
	'6':      true,
	'7':      true,
	'8':      true,
	'9':      true,
	':':      true,
	';':      true,
	'<':      true,
	'=':      true,
	'>':      true,
	'?':      true,
	'@':      true,
	'A':      true,
	'B':      true,
	'C':      true,
	'D':      true,
	'E':      true,
	'F':      true,
	'G':      true,
	'H':      true,
	'I':      true,
	'J':      true,
	'K':      true,
	'L':      true,
	'M':      true,
	'N':      true,
	'O':      true,
	'P':      true,
	'Q':      true,
	'R':      true,
	'S':      true,
	'T':      true,
	'U':      true,
	'V':      true,
	'W':      true,
	'X':      true,
	'Y':      true,
	'Z':      true,
	'[':      true,
	'\\':     false,
	']':      true,
	'^':      true,
	'_':      true,
	'`':      true,
	'a':      true,
	'b':      true,
	'c':      true,
	'd':      true,
	'e':      true,
	'f':      true,
	'g':      true,
	'h':      true,
	'i':      true,
	'j':      true,
	'k':      true,
	'l':      true,
	'm':      true,
	'n':      true,
	'o':      true,
	'p':      true,
	'q':      true,
	'r':      true,
	's':      true,
	't':      true,
	'u':      true,
	'v':      true,
	'w':      true,
	'x':      true,
	'y':      true,
	'z':      true,
	'{':      true,
	'|':      true,
	'}':      true,
	'~':      true,
	'\u007f': true,
}
