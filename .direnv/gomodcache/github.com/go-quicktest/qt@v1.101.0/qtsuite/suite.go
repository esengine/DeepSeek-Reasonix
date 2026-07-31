// Licensed under the MIT license, see LICENSE file for details.

/*
Package qtsuite allows quicktest to run test suites.

A test suite is a value with one or more test methods.
For example, the following code defines a suite of test functions that starts
an HTTP server before running each test, and tears it down afterwards:

	type suite struct {
		url string
	}

	func (s *suite) Init(t *testing.T) {
		hnd := func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprintf(w, "%s %s", req.Method, req.URL.Path)
		}
		srv := httptest.NewServer(http.HandlerFunc(hnd))
		t.Cleanup(srv.Close)
		s.url = srv.URL
	}

	func (s *suite) TestGet(t *testing.T) {
		t.Parallel()
		resp, err := http.Get(s.url)
		qt.Assert(t, qt.IsNil(err))
		defer resp.Body.Close()
		b, err := ioutil.ReadAll(resp.Body)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(b), "GET /"))
	}

	func (s *suite) TestHead(t *testing.T) {
		t.Parallel()
		resp, err := http.Head(s.url + "/path")
		qt.Assert(t, qt.IsNil(err))
		defer resp.Body.Close()
		b, err := ioutil.ReadAll(resp.Body)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(b), ""))
		qt.Assert(t, qt.Equals(resp.ContentLength, 10))
	}

The above code could be invoked from a test function like this:

	func TestHTTPMethods(t *testing.T) {
		qtsuite.Run(t, &suite{})
	}
*/
package qtsuite

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// Run runs each test method defined on the given value as a separate
// subtest. A test is a method of the form
//
//	func (T) TestXxx(*testing.T)
//
// where Xxx does not start with a lowercase letter.
//
// If suite is a pointer, the value pointed to is copied before any
// methods are invoked on it: a new copy is made for each test. This
// means that it is OK for tests to modify fields in suite concurrently
// if desired - it's OK to call t.Parallel().
//
// If suite has a method of the form
//
//	func (T) Init(*testing.T)
//
// this method will be invoked before each test run.
func Run(t *testing.T, suite any) {
	sv := reflect.ValueOf(suite)
	st := sv.Type()
	init, hasInit := st.MethodByName("Init")
	if hasInit && !isValidMethod(init) {
		t.Fatal("wrong signature for Init, must be Init(*testing.T)")
	}
	for i := 0; i < st.NumMethod(); i++ {
		m := st.Method(i)
		if !isTestMethod(m) {
			continue
		}
		t.Run(m.Name, func(t *testing.T) {
			if !isValidMethod(m) {
				t.Fatalf("wrong signature for %s, must be %s(*testing.T)", m.Name, m.Name)
			}

			sv := sv
			if st.Kind() == reflect.Ptr {
				sv1 := reflect.New(st.Elem())
				sv1.Elem().Set(sv.Elem())
				sv = sv1
			}
			args := []reflect.Value{sv, reflect.ValueOf(t)}
			if hasInit {
				init.Func.Call(args)
			}
			m.Func.Call(args)
		})
	}
}

var tType = reflect.TypeOf((*testing.T)(nil))

func isTestMethod(m reflect.Method) bool {
	if !strings.HasPrefix(m.Name, "Test") {
		return false
	}
	r, n := utf8.DecodeRuneInString(m.Name[4:])
	return n == 0 || !unicode.IsLower(r)
}

func isValidMethod(m reflect.Method) bool {
	return m.Type.NumIn() == 2 && m.Type.NumOut() == 0 && m.Type.In(1) == tType
}
