// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package knownhosts

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

const edKeyStr = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGBAarftlLeoyf+v+nVchEZII/vna2PCV8FaX4vsF5BX"
const alternateEdKeyStr = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIXffBYeYL+WVzVru8npl5JHt2cjlr4ornFTWzoij9sx"
const ecKeyStr = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBNLCu01+wpXe3xB5olXCN4SqU2rQu0qjSRKJO4Bg+JRCPU+ENcgdA5srTU8xYDz/GEa4dzK5ldPw4J/gZgSXCMs="

var ecKey, alternateEdKey, edKey ssh.PublicKey
var testAddr = &net.TCPAddr{
	IP:   net.IP{198, 41, 30, 196},
	Port: 22,
}

var testAddr6 = &net.TCPAddr{
	IP: net.IP{198, 41, 30, 196,
		1, 2, 3, 4,
		1, 2, 3, 4,
		1, 2, 3, 4,
	},
	Port: 22,
}

func init() {
	var err error
	ecKey, _, _, _, err = ssh.ParseAuthorizedKey([]byte(ecKeyStr))
	if err != nil {
		panic(err)
	}
	edKey, _, _, _, err = ssh.ParseAuthorizedKey([]byte(edKeyStr))
	if err != nil {
		panic(err)
	}
	alternateEdKey, _, _, _, err = ssh.ParseAuthorizedKey([]byte(alternateEdKeyStr))
	if err != nil {
		panic(err)
	}
}

func testDB(t *testing.T, s string) *hostKeyDB {
	db := newHostKeyDB()
	if err := db.Read(bytes.NewBufferString(s), "testdb"); err != nil {
		t.Fatalf("Read: %v", err)
	}

	return db
}

func TestRevoked(t *testing.T) {
	db := testDB(t, "\n\n@revoked * "+edKeyStr+"\n")
	want := &RevokedError{
		Revoked: KnownKey{
			Key:      edKey,
			Filename: "testdb",
			Line:     3,
		},
	}
	if err := db.check("", &net.TCPAddr{
		Port: 42,
	}, edKey); err == nil {
		t.Fatal("no error for revoked key")
	} else if !reflect.DeepEqual(want, err) {
		t.Fatalf("got %#v, want %#v", want, err)
	}
}

func TestHostAuthority(t *testing.T) {
	for _, m := range []struct {
		authorityFor string
		address      string

		good bool
	}{
		{authorityFor: "localhost", address: "localhost:22", good: true},
		{authorityFor: "localhost", address: "localhost", good: false},
		{authorityFor: "localhost", address: "localhost:1234", good: false},
		{authorityFor: "[localhost]:1234", address: "localhost:1234", good: true},
		{authorityFor: "[localhost]:1234", address: "localhost:22", good: false},
		{authorityFor: "[localhost]:1234", address: "localhost", good: false},
	} {
		db := testDB(t, `@cert-authority `+m.authorityFor+` `+edKeyStr)
		if ok := db.IsHostAuthority(db.lines[0].knownKey.Key, m.address); ok != m.good {
			t.Errorf("IsHostAuthority: authority %s, address %s, wanted good = %v, got good = %v",
				m.authorityFor, m.address, m.good, ok)
		}
	}
}

func TestBracket(t *testing.T) {
	db := testDB(t, `[git.eclipse.org]:29418,[198.41.30.196]:29418 `+edKeyStr)

	if err := db.check("git.eclipse.org:29418", &net.TCPAddr{
		IP:   net.IP{198, 41, 30, 196},
		Port: 29418,
	}, edKey); err != nil {
		t.Errorf("got error %v, want none", err)
	}

	if err := db.check("git.eclipse.org:29419", &net.TCPAddr{
		Port: 42,
	}, edKey); err == nil {
		t.Fatalf("no error for unknown address")
	} else if ke, ok := err.(*KeyError); !ok {
		t.Fatalf("got type %T, want *KeyError", err)
	} else if len(ke.Want) > 0 {
		t.Fatalf("got Want %v, want []", ke.Want)
	}
}

func TestNewKeyType(t *testing.T) {
	str := fmt.Sprintf("%s %s", testAddr, edKeyStr)
	db := testDB(t, str)
	if err := db.check("", testAddr, ecKey); err == nil {
		t.Fatalf("no error for unknown address")
	} else if ke, ok := err.(*KeyError); !ok {
		t.Fatalf("got type %T, want *KeyError", err)
	} else if len(ke.Want) == 0 {
		t.Fatalf("got empty KeyError.Want")
	}
}

func TestSameKeyType(t *testing.T) {
	str := fmt.Sprintf("%s %s", testAddr, edKeyStr)
	db := testDB(t, str)
	if err := db.check("", testAddr, alternateEdKey); err == nil {
		t.Fatalf("no error for unknown address")
	} else if ke, ok := err.(*KeyError); !ok {
		t.Fatalf("got type %T, want *KeyError", err)
	} else if len(ke.Want) == 0 {
		t.Fatalf("got empty KeyError.Want")
	} else if got, want := ke.Want[0].Key.Marshal(), edKey.Marshal(); !bytes.Equal(got, want) {
		t.Fatalf("got key %q, want %q", got, want)
	}
}

func TestIPAddress(t *testing.T) {
	str := fmt.Sprintf("%s %s", testAddr, edKeyStr)
	db := testDB(t, str)
	if err := db.check("", testAddr, edKey); err != nil {
		t.Errorf("got error %q, want none", err)
	}
}

func TestIPv6Address(t *testing.T) {
	str := fmt.Sprintf("%s %s", testAddr6, edKeyStr)
	db := testDB(t, str)

	if err := db.check("", testAddr6, edKey); err != nil {
		t.Errorf("got error %q, want none", err)
	}
}

func TestBasic(t *testing.T) {
	str := fmt.Sprintf("#comment\n\nserver.org,%s %s\notherhost %s", testAddr, edKeyStr, ecKeyStr)
	db := testDB(t, str)
	if err := db.check("server.org:22", testAddr, edKey); err != nil {
		t.Errorf("got error %v, want none", err)
	}

	want := KnownKey{
		Key:      edKey,
		Filename: "testdb",
		Line:     3,
	}
	if err := db.check("server.org:22", testAddr, ecKey); err == nil {
		t.Errorf("succeeded, want KeyError")
	} else if ke, ok := err.(*KeyError); !ok {
		t.Errorf("got %T, want *KeyError", err)
	} else if len(ke.Want) != 1 {
		t.Errorf("got %v, want 1 entry", ke)
	} else if !reflect.DeepEqual(ke.Want[0], want) {
		t.Errorf("got %v, want %v", ke.Want[0], want)
	}
}

func TestHostNamePrecedence(t *testing.T) {
	var evilAddr = &net.TCPAddr{
		IP:   net.IP{66, 66, 66, 66},
		Port: 22,
	}

	str := fmt.Sprintf("server.org,%s %s\nevil.org,%s %s", testAddr, edKeyStr, evilAddr, ecKeyStr)
	db := testDB(t, str)

	if err := db.check("server.org:22", evilAddr, ecKey); err == nil {
		t.Errorf("check succeeded")
	} else if _, ok := err.(*KeyError); !ok {
		t.Errorf("got %T, want *KeyError", err)
	}
}

func TestNegate(t *testing.T) {
	str := fmt.Sprintf("%s,!server.org %s", testAddr, edKeyStr)
	db := testDB(t, str)
	if err := db.check("server.org:22", testAddr, ecKey); err == nil {
		t.Errorf("succeeded")
	} else if ke, ok := err.(*KeyError); !ok {
		t.Errorf("got error type %T, want *KeyError", err)
	} else if len(ke.Want) != 0 {
		t.Errorf("got expected keys %d (first of type %s), want []", len(ke.Want), ke.Want[0].Key.Type())
	}
}

func TestWildcard(t *testing.T) {
	str := fmt.Sprintf("server*.domain %s", edKeyStr)
	db := testDB(t, str)

	want := &KeyError{
		Want: []KnownKey{{
			Filename: "testdb",
			Line:     1,
			Key:      edKey,
		}},
	}

	got := db.check("server.domain:22", &net.TCPAddr{}, ecKey)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestLine(t *testing.T) {
	for in, want := range map[string]string{
		"server.org":                             "server.org " + edKeyStr,
		"server.org:22":                          "server.org " + edKeyStr,
		"server.org:23":                          "[server.org]:23 " + edKeyStr,
		"[c629:1ec4:102:304:102:304:102:304]:22": "c629:1ec4:102:304:102:304:102:304 " + edKeyStr,
		"[c629:1ec4:102:304:102:304:102:304]:23": "[c629:1ec4:102:304:102:304:102:304]:23 " + edKeyStr,
	} {
		if got := Line([]string{in}, edKey); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWildcardMatch(t *testing.T) {
	for _, c := range []struct {
		pat, str string
		want     bool
	}{
		{"a?b", "abb", true},
		{"ab", "abc", false},
		{"abc", "ab", false},
		{"a*b", "axxxb", true},
		{"a*b", "axbxb", true},
		{"a*b", "axbxbc", false},
		{"a*?", "axbxc", true},
		{"a*b*", "axxbxxxxxx", true},
		{"a*b*c", "axxbxxxxxxc", true},
		{"a*b*?", "axxbxxxxxxc", true},
		{"a*b*z", "axxbxxbxxxz", true},
		{"a*b*z", "axxbxxzxxxz", true},
		{"a*b*z", "axxbxxzxxx", false},
	} {
		got := wildcardMatch([]byte(c.pat), []byte(c.str))
		if got != c.want {
			t.Errorf("wildcardMatch(%q, %q) = %v, want %v", c.pat, c.str, got, c.want)
		}

	}
}

func TestRevokedCA(t *testing.T) {
	_, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caSigner, err := ssh.NewSignerFromKey(caPriv)
	if err != nil {
		t.Fatal(err)
	}
	caKey := caSigner.PublicKey()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewPublicKey(hostPriv.Public())
	if err != nil {
		t.Fatal(err)
	}

	cert := &ssh.Certificate{
		CertType:        ssh.HostCert,
		Key:             hostKey,
		ValidBefore:     ssh.CertTimeInfinity,
		ValidPrincipals: []string{"server.org"},
	}
	if err := cert.SignCert(rand.Reader, caSigner); err != nil {
		t.Fatal(err)
	}

	caLine := "ssh-ed25519 " + serialize(caKey)[len("ssh-ed25519 "):]
	knownHostsData := "@revoked server.org " + caLine + "\n" +
		"@cert-authority server.org " + caLine + "\n"
	db := testDB(t, knownHostsData)

	if !db.IsRevoked(cert) {
		t.Error("IsRevoked returned false for certificate signed by revoked CA")
	}
}

const testHostname = "hostname"

// generated with keygen -H -f
const encodedTestHostnameHash = "|1|IHXZvQMvTcZTUU29+2vXFgx8Frs=|UGccIWfRVDwilMBnA3WJoRAC75Y="

func TestHostHash(t *testing.T) {
	testHostHash(t, testHostname, encodedTestHostnameHash)
}

func TestHashList(t *testing.T) {
	encoded := HashHostname(testHostname)
	testHostHash(t, testHostname, encoded)
}

func testHostHash(t *testing.T, hostname, encoded string) {
	typ, salt, hash, err := decodeHash(encoded)
	if err != nil {
		t.Fatalf("decodeHash: %v", err)
	}

	if got := encodeHash(typ, salt, hash); got != encoded {
		t.Errorf("got encoding %s want %s", got, encoded)
	}

	if typ != sha1HashType {
		t.Fatalf("got hash type %q, want %q", typ, sha1HashType)
	}

	got := hashHost(hostname, salt)
	if !bytes.Equal(got, hash) {
		t.Errorf("got hash %x want %x", got, hash)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1":                 "127.0.0.1",
		"127.0.0.1:22":              "127.0.0.1",
		"[127.0.0.1]:22":            "127.0.0.1",
		"[127.0.0.1]:23":            "[127.0.0.1]:23",
		"127.0.0.1:23":              "[127.0.0.1]:23",
		"[a.b.c]:22":                "a.b.c",
		"[a.b.c]:23":                "[a.b.c]:23",
		"abcd::abcd:abcd:abcd":      "abcd::abcd:abcd:abcd",
		"[abcd::abcd:abcd:abcd]":    "abcd::abcd:abcd:abcd",
		"[abcd::abcd:abcd:abcd]:22": "abcd::abcd:abcd:abcd",
		"[abcd::abcd:abcd:abcd]:23": "[abcd::abcd:abcd:abcd]:23",
		"2001:db8::1":               "2001:db8::1",
		"2001:db8::1:22":            "2001:db8::1:22",
		"[2001:db8::1]:22":          "2001:db8::1",
		"2001:db8::1:2200":          "2001:db8::1:2200",
		"a.b.c.d.com:2200":          "[a.b.c.d.com]:2200",
		"2001::db8:1":               "2001::db8:1",
		"2001::db8:1:22":            "2001::db8:1:22",
		"2001::db8:1:2200":          "2001::db8:1:2200",
	} {
		got := Normalize(in)
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHashedHostkeyCheck(t *testing.T) {
	str := fmt.Sprintf("%s %s", HashHostname(testHostname), edKeyStr)
	db := testDB(t, str)
	if err := db.check(testHostname+":22", testAddr, edKey); err != nil {
		t.Errorf("check(%s): %v", testHostname, err)
	}
	want := &KeyError{
		Want: []KnownKey{{
			Filename: "testdb",
			Line:     1,
			Key:      edKey,
		}},
	}
	if got := db.check(testHostname+":22", testAddr, alternateEdKey); !reflect.DeepEqual(got, want) {
		t.Errorf("got error %v, want %v", got, want)
	}
}

func TestIssue36126(t *testing.T) {
	str := fmt.Sprintf("server.org,%s %s\nserver.org,%s %s", testAddr, edKeyStr, testAddr, alternateEdKeyStr)
	db := testDB(t, str)

	if err := db.check("server.org:22", testAddr, edKey); err != nil {
		t.Errorf("should have passed the check, got %v", err)
	}

	if err := db.check("server.org:22", testAddr, alternateEdKey); err != nil {
		t.Errorf("should have passed the check, got %v", err)
	}
}

func TestUnicodeSpace(t *testing.T) {
	line := fmt.Sprintf("server.org %s  %s", edKey.Type(), base64.StdEncoding.EncodeToString(edKey.Marshal()))

	db := newHostKeyDB()
	err := db.Read(bytes.NewBufferString(line), "testdb")

	if err == nil {
		t.Fatal("Read succeeded on line with Unicode space, expected error due to strict ASCII parsing")
	}
}

func TestUnicodeSpaceTrimming(t *testing.T) {
	const unicodeSpace = " "
	line := fmt.Sprintf("%sserver.org %s", unicodeSpace, edKeyStr)

	db := newHostKeyDB()
	err := db.Read(bytes.NewBufferString(line), "testdb")

	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	lines := db.lines
	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	cleanAddr := addr{host: "server.org", port: "22"}
	dirtyAddr := addr{host: unicodeSpace + "server.org", port: "22"}

	if lines[0].match(cleanAddr) {
		t.Errorf("Matched clean host 'server.org', implying Unicode space was trimmed")
	}
	if !lines[0].match(dirtyAddr) {
		t.Errorf("Did not match dirty host, implying parsing issue")
	}
}

func TestKeyTypeMismatch(t *testing.T) {
	line := fmt.Sprintf("server.org ssh-rsa %s", base64.StdEncoding.EncodeToString(edKey.Marshal()))

	db := newHostKeyDB()
	err := db.Read(bytes.NewBufferString(line), "testdb")
	if err == nil {
		t.Fatal("Read succeeded on line with key type mismatch, expected error")
	}

	expectedErr := `knownhosts: key type mismatch: found "ssh-ed25519", want "ssh-rsa"`
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("got error %q, want to contain %q", err.Error(), expectedErr)
	}
}

func TestMultipleMarkers(t *testing.T) {
	lineDouble := fmt.Sprintf("@cert-authority @revoked server.org %s %s", edKey.Type(), base64.StdEncoding.EncodeToString(edKey.Marshal()))
	db := newHostKeyDB()
	err := db.Read(bytes.NewBufferString(lineDouble), "testdb")
	if err == nil {
		t.Fatal("Read succeeded on line with multiple markers, expected error")
	}
	expectedError := "unexpected marker"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("got error %q, want it to contain %q", err.Error(), expectedError)
	}

	lineUnknown := fmt.Sprintf("@unknown-marker server.org %s %s", edKey.Type(), base64.StdEncoding.EncodeToString(edKey.Marshal()))
	err = db.Read(bytes.NewBufferString(lineUnknown), "testdb")
	if err == nil {
		t.Fatal("Read succeeded on line with unknown marker, expected error")
	}
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("got error %q, want it to contain %q", err.Error(), expectedError)
	}
}

func TestUnknownMarker(t *testing.T) {
	line := fmt.Sprintf("@unknown-marker server.org %s %s", edKey.Type(), base64.StdEncoding.EncodeToString(edKey.Marshal()))

	db := newHostKeyDB()
	err := db.Read(bytes.NewBufferString(line), "testdb")

	if err == nil {
		t.Fatal("Read succeeded on line with unknown marker, expected error")
	}

	expectedError := "unexpected marker"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("got error %q, want it to contain %q", err.Error(), expectedError)
	}
}
