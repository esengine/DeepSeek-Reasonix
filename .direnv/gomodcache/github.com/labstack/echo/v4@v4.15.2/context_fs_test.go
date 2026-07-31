// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"github.com/stretchr/testify/assert"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestContext_File(t *testing.T) {
	var testCases = []struct {
		name             string
		whenFile         string
		whenFS           fs.FS
		expectStatus     int
		expectStartsWith []byte
		expectError      string
	}{
		{
			name:             "ok, from default file system",
			whenFile:         "_fixture/images/walle.png",
			whenFS:           nil,
			expectStatus:     http.StatusOK,
			expectStartsWith: []byte{0x89, 0x50, 0x4e},
		},
		{
			name:             "ok, from custom file system",
			whenFile:         "walle.png",
			whenFS:           os.DirFS("_fixture/images"),
			expectStatus:     http.StatusOK,
			expectStartsWith: []byte{0x89, 0x50, 0x4e},
		},
		{
			name:             "nok, not existent file",
			whenFile:         "not.png",
			whenFS:           os.DirFS("_fixture/images"),
			expectStatus:     http.StatusOK,
			expectStartsWith: nil,
			expectError:      "code=404, message=Not Found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			if tc.whenFS != nil {
				e.Filesystem = tc.whenFS
			}

			handler := func(ec Context) error {
				return ec.(*context).File(tc.whenFile)
			}

			req := httptest.NewRequest(http.MethodGet, "/match.png", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler(c)

			assert.Equal(t, tc.expectStatus, rec.Code)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}

			body := rec.Body.Bytes()
			if len(body) > len(tc.expectStartsWith) {
				body = body[:len(tc.expectStartsWith)]
			}
			assert.Equal(t, tc.expectStartsWith, body)
		})
	}
}

func TestContext_FileFS(t *testing.T) {
	var testCases = []struct {
		name             string
		whenFile         string
		whenFS           fs.FS
		expectStatus     int
		expectStartsWith []byte
		expectError      string
	}{
		{
			name:             "ok",
			whenFile:         "walle.png",
			whenFS:           os.DirFS("_fixture/images"),
			expectStatus:     http.StatusOK,
			expectStartsWith: []byte{0x89, 0x50, 0x4e},
		},
		{
			name:             "nok, not existent file",
			whenFile:         "not.png",
			whenFS:           os.DirFS("_fixture/images"),
			expectStatus:     http.StatusOK,
			expectStartsWith: nil,
			expectError:      "code=404, message=Not Found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()

			handler := func(ec Context) error {
				return ec.(*context).FileFS(tc.whenFile, tc.whenFS)
			}

			req := httptest.NewRequest(http.MethodGet, "/match.png", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler(c)

			assert.Equal(t, tc.expectStatus, rec.Code)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}

			body := rec.Body.Bytes()
			if len(body) > len(tc.expectStartsWith) {
				body = body[:len(tc.expectStartsWith)]
			}
			assert.Equal(t, tc.expectStartsWith, body)
		})
	}
}
