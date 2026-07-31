/*
 * MIT License
 *
 * Copyright (c) 2022 Lark Technologies Pte. Ltd.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice, shall be included in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package larkcore

import (
	"runtime/debug"
	"sync"
)

const (
	sdkModulePath   = "github.com/larksuite/oapi-sdk-go/v3"
	fallbackVersion = "unknown"
)

var (
	sdkVersionOnce  sync.Once
	resolvedVersion string
)

// SDKVersion returns the SDK module version recorded in the built binary.
func SDKVersion() string {
	sdkVersionOnce.Do(func() {
		resolvedVersion = fallbackVersion
		info, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		if info.Main.Path == sdkModulePath && validModuleVersion(info.Main.Version) {
			resolvedVersion = info.Main.Version
			return
		}
		for _, dep := range info.Deps {
			if dep.Path != sdkModulePath {
				continue
			}
			if validModuleVersion(dep.Version) {
				resolvedVersion = dep.Version
				return
			}
			if dep.Replace != nil && validModuleVersion(dep.Replace.Version) {
				resolvedVersion = dep.Replace.Version
				return
			}
		}
	})
	return resolvedVersion
}

func validModuleVersion(version string) bool {
	return version != "" && version != "(devel)"
}
