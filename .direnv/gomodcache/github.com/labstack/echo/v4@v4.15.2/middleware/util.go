// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
)

func matchScheme(domain, pattern string) bool {
	didx := strings.Index(domain, ":")
	pidx := strings.Index(pattern, ":")
	return didx != -1 && pidx != -1 && domain[:didx] == pattern[:pidx]
}

// matchSubdomain compares authority with wildcard
func matchSubdomain(domain, pattern string) bool {
	if !matchScheme(domain, pattern) {
		return false
	}
	didx := strings.Index(domain, "://")
	pidx := strings.Index(pattern, "://")
	if didx == -1 || pidx == -1 {
		return false
	}
	domAuth := domain[didx+3:]
	// to avoid long loop by invalid long domain
	if len(domAuth) > 253 {
		return false
	}
	patAuth := pattern[pidx+3:]

	domComp := strings.Split(domAuth, ".")
	patComp := strings.Split(patAuth, ".")
	for i := len(domComp)/2 - 1; i >= 0; i-- {
		opp := len(domComp) - 1 - i
		domComp[i], domComp[opp] = domComp[opp], domComp[i]
	}
	for i := len(patComp)/2 - 1; i >= 0; i-- {
		opp := len(patComp) - 1 - i
		patComp[i], patComp[opp] = patComp[opp], patComp[i]
	}

	for i, v := range domComp {
		if len(patComp) <= i {
			return false
		}
		p := patComp[i]
		if p == "*" {
			return true
		}
		if p != v {
			return false
		}
	}
	return false
}

// https://tip.golang.org/doc/go1.19#:~:text=Read%20no%20longer%20buffers%20random%20data%20obtained%20from%20the%20operating%20system%20between%20calls
var randomReaderPool = sync.Pool{New: func() interface{} {
	return bufio.NewReader(rand.Reader)
}}

const randomStringCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const randomStringCharsetLen = 52 // len(randomStringCharset)
const randomStringMaxByte = 255 - (256 % randomStringCharsetLen)

func randomString(length uint8) string {
	reader := randomReaderPool.Get().(*bufio.Reader)
	defer randomReaderPool.Put(reader)

	b := make([]byte, length)
	r := make([]byte, length+(length/4)) // perf: avoid read from rand.Reader many times
	var i uint8 = 0

	// security note:
	// we can't just simply do b[i]=randomStringCharset[rb%len(randomStringCharset)],
	// len(len(randomStringCharset)) is 52, and rb is [0, 255], 256 = 52 * 4 + 48.
	// make the first 48 characters more possibly to be generated then others.
	// So we have to skip bytes when rb > randomStringMaxByte

	for {
		_, err := io.ReadFull(reader, r)
		if err != nil {
			panic("unexpected error happened when reading from bufio.NewReader(crypto/rand.Reader)")
		}
		for _, rb := range r {
			if rb > randomStringMaxByte {
				// Skip this number to avoid bias.
				continue
			}
			b[i] = randomStringCharset[rb%randomStringCharsetLen]
			i++
			if i == length {
				return string(b)
			}
		}
	}
}

func validateOrigins(origins []string, what string) error {
	for _, o := range origins {
		if err := validateOrigin(o, what); err != nil {
			return err
		}
	}
	return nil
}

func validateOrigin(origin string, what string) error {
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("can not parse %s: %w", what, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s is missing scheme or host: %s", what, origin)
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s can not have path, query, and fragments: %s", what, origin)
	}
	return nil
}
