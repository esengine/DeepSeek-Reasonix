// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zip

import "testing"

var pre124 []string = []string{
	"",
	"go1.14",
	"go1.21.0",
	"go1.22.4",
	"go1.23",
	"go1.23.1",
	"go1.2",
	"go1.7",
	"go1.9",
}

var after124 []string = []string{"go1.24.0", "go1.24", "go1.99.0"}

var allVers []string = append(pre124, after124...)

func TestIsVendoredPackage(t *testing.T) {
	for _, tc := range []struct {
		path     string
		want     bool
		versions []string
	}{
		{path: "vendor/foo/foo.go", want: true, versions: allVers},
		{path: "pkg/vendor/foo/foo.go", want: true, versions: allVers},
		{path: "longpackagename/vendor/foo/foo.go", want: true, versions: allVers},
		{path: "vendor/vendor.go", want: false, versions: allVers},
		{path: "vendor/foo/modules.txt", want: true, versions: allVers},
		{path: "modules.txt", want: false, versions: allVers},
		{path: "vendor/amodules.txt", want: false, versions: allVers},

		// These test cases were affected by https://golang.org/issue/63395
		{path: "vendor/modules.txt", want: false, versions: pre124},
		{path: "vendor/modules.txt", want: true, versions: after124},

		// These test cases were affected by https://golang.org/issue/37397
		{path: "pkg/vendor/vendor.go", want: true, versions: pre124},
		{path: "pkg/vendor/vendor.go", want: false, versions: after124},
		{path: "longpackagename/vendor/vendor.go", want: true, versions: pre124},
		{path: "longpackagename/vendor/vendor.go", want: false, versions: after124},
	} {
		for _, v := range tc.versions {
			got := isVendoredPackage(tc.path, v)
			want := tc.want
			if got != want {
				t.Errorf("isVendoredPackage(%q, %s) = %t; want %t", tc.path, v, got, tc.want)
			}
		}
	}
}
