// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package testcert contains a test-only localhost certificate.
package testcert

import (
	"strings"
)

// LocalhostCert is a PEM-encoded TLS cert with SAN IPs
// "127.0.0.1" and "[::1]", expiring at Jan 29 16:00:00 2084 GMT.
// generated from src/crypto/tls:
// go run generate_cert.go  --ecdsa-curve P256 --host 127.0.0.1,::1,example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
var LocalhostCert = []byte(`-----BEGIN CERTIFICATE-----
MIIBrDCCAVKgAwIBAgIPCvPhO+Hfv+NW76kWxULUMAoGCCqGSM49BAMCMBIxEDAO
BgNVBAoTB0FjbWUgQ28wIBcNNzAwMTAxMDAwMDAwWhgPMjA4NDAxMjkxNjAwMDBa
MBIxEDAOBgNVBAoTB0FjbWUgQ28wWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAARh
WRF8p8X9scgW7JjqAwI9nYV8jtkdhqAXG9gyEgnaFNN5Ze9l3Tp1R9yCDBMNsGms
PyfMPe5Jrha/LmjgR1G9o4GIMIGFMA4GA1UdDwEB/wQEAwIChDATBgNVHSUEDDAK
BggrBgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBSOJri/wLQxq6oC
Y6ZImms/STbTljAuBgNVHREEJzAlggtleGFtcGxlLmNvbYcEfwAAAYcQAAAAAAAA
AAAAAAAAAAAAATAKBggqhkjOPQQDAgNIADBFAiBUguxsW6TGhixBAdORmVNnkx40
HjkKwncMSDbUaeL9jQIhAJwQ8zV9JpQvYpsiDuMmqCuW35XXil3cQ6Drz82c+fvE
-----END CERTIFICATE-----`)

// LocalhostKey is the private key for localhostCert.
var LocalhostKey = []byte(testingKey(`-----BEGIN TESTING KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgY1B1eL/Bbwf/MDcs
rnvvWhFNr1aGmJJR59PdCN9lVVqhRANCAARhWRF8p8X9scgW7JjqAwI9nYV8jtkd
hqAXG9gyEgnaFNN5Ze9l3Tp1R9yCDBMNsGmsPyfMPe5Jrha/LmjgR1G9
-----END TESTING KEY-----`))

// testingKey helps keep security scanners from getting excited about a private key in this file.
func testingKey(s string) string { return strings.ReplaceAll(s, "TESTING KEY", "PRIVATE KEY") }
