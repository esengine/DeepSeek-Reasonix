// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Ensures that we can catch any regressions with nil dereferences
// from const declarations in other files within the same package.
// See issue https://golang.org/issues/60555
func main() {
	p := message.NewPrinter(language.English)
	p.Printf(testMessage)
}
