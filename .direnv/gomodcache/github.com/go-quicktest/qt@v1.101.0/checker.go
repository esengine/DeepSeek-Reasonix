// Licensed under the MIT license, see LICENSE file for details.

package qt

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kr/pretty"
)

// Checker is implemented by types used as part of Check/Assert invocations.
type Checker interface {
	// Check runs the check for this checker.
	// On failure, the returned error is printed along with
	// the checker arguments (obtained by calling Args)
	// and key-value pairs added by calling the note function.
	//
	// If Check returns ErrSilent, neither the checker arguments nor
	// the error are printed; values with note are still printed.
	Check(note func(key string, value any)) error

	// Args returns a slice of all the arguments passed
	// to the checker. The first argument should always be
	// the "got" value being checked.
	Args() []Arg
}

// Arg holds a single argument to a checker.
type Arg struct {
	Name  string
	Value any
}

// negatedError is implemented on checkers that want to customize the error that
// is returned when they have succeeded but that success has been negated.
type negatedError interface {
	negatedError() error
}

// Equals returns a Checker checking equality of two comparable values.
//
// Note that T is not constrained to be comparable because
// we also allow comparing interface values which currently
// do not satisfy that constraint.
func Equals[T any](got, want T) Checker {
	return &equalsChecker[T]{argPairOf(got, want)}
}

type equalsChecker[T any] struct {
	argPair[T, T]
}

func (c *equalsChecker[T]) Check(note func(key string, value any)) (err error) {
	defer func() {
		// A panic is raised when the provided values are interfaces containing
		// non-comparable values.
		if r := recover(); r != nil {
			err = fmt.Errorf("%s", r)
		}
	}()

	if any(c.got) == any(c.want) {
		return nil
	}

	// Customize error message for non-nil errors.
	if typeOf[T]() == typeOf[error]() {
		if any(c.want) == nil {
			return errors.New("got non-nil error")
		}
		if any(c.got) == nil {
			return errors.New("got nil error")
		}
		// Show error types when comparing errors with different types.
		gotType := reflect.TypeOf(c.got)
		wantType := reflect.TypeOf(c.want)
		if gotType != wantType {
			note("got type", Unquoted(gotType.String()))
			note("want type", Unquoted(wantType.String()))
		}
		return errors.New("values are not equal")
	}

	// Show line diff when comparing different multi-line strings.
	if c, ok := any(c).(*equalsChecker[string]); ok {
		isMultiLine := func(s string) bool {
			i := strings.Index(s, "\n")
			return i != -1 && i < len(s)-1
		}
		if isMultiLine(c.got) || isMultiLine(c.want) {
			diff := cmp.Diff(strings.SplitAfter(c.got, "\n"), strings.SplitAfter(c.want, "\n"))
			note("line diff (-got +want)", Unquoted(diff))
		}
	}

	return errors.New("values are not equal")
}

// DeepEquals returns a Checker checking equality of two values
// using cmp.DeepEqual.
func DeepEquals[T any](got, want T) Checker {
	return CmpEquals(got, want)
}

// CmpEquals is like DeepEquals but allows custom compare options
// to be passed too, to allow unexported fields to be compared.
//
// It can be useful to define your own version that uses a custom
// set of compare options. See example for details.
func CmpEquals[T any](got, want T, opts ...cmp.Option) Checker {
	return &cmpEqualsChecker[T]{
		argPair: argPairOf(got, want),
		opts:    opts,
	}
}

type cmpEqualsChecker[T any] struct {
	argPair[T, T]
	opts []cmp.Option
}

func (c *cmpEqualsChecker[T]) Check(note func(key string, value any)) (err error) {
	defer func() {
		// A panic is raised in some cases, for instance when trying to compare
		// structs with unexported fields and neither AllowUnexported nor
		// cmpopts.IgnoreUnexported are provided.
		if r := recover(); r != nil {
			err = BadCheckf("%s", r)
		}
	}()
	if diff := cmp.Diff(c.got, c.want, c.opts...); diff != "" {
		// Only output values when the verbose flag is set.
		note("error", Unquoted("values are not deep equal"))
		note("diff (-got +want)", Unquoted(diff))
		note("got", SuppressedIfLong{c.got})
		note("want", SuppressedIfLong{c.want})
		return ErrSilent
	}
	return nil
}

// ContentEquals is like DeepEquals but any slices in the compared values will
// be sorted before being compared.
func ContentEquals[T any](got, want T) Checker {
	return CmpEquals(got, want, cmpopts.SortSlices(func(x, y any) bool {
		// TODO frankban: implement a proper sort function.
		return pretty.Sprint(x) < pretty.Sprint(y)
	}))
}

// Matches returns a Checker checking that the provided string matches the
// provided regular expression pattern.
func Matches[StringOrRegexp string | *regexp.Regexp](got string, want StringOrRegexp) Checker {
	return &matchesChecker{
		got:   got,
		want:  want,
		match: newMatcher(want),
	}
}

type matchesChecker struct {
	got   string
	want  any
	match matcher
}

func (c *matchesChecker) Check(note func(key string, value any)) error {
	return c.match(c.got, "value does not match regexp", note)
}

func (c *matchesChecker) Args() []Arg {
	return []Arg{{Name: "got value", Value: c.got}, {Name: "regexp", Value: c.want}}
}

// ErrorMatches returns a Checker checking that the provided value is an error
// whose message matches the provided regular expression pattern.
func ErrorMatches[StringOrRegexp string | *regexp.Regexp](got error, want StringOrRegexp) Checker {
	return &errorMatchesChecker{
		got:   got,
		want:  want,
		match: newMatcher(want),
	}
}

type errorMatchesChecker struct {
	got   error
	want  any
	match matcher
}

func (c *errorMatchesChecker) Check(note func(key string, value any)) error {
	if c.got == nil {
		return errors.New("got nil error but want non-nil")
	}
	return c.match(c.got.Error(), "error does not match regexp", note)
}

func (c *errorMatchesChecker) Args() []Arg {
	return []Arg{{Name: "got error", Value: c.got}, {Name: "regexp", Value: c.want}}
}

// PanicMatches returns a Checker checking that the provided function panics
// with a message matching the provided regular expression pattern.
func PanicMatches[StringOrRegexp string | *regexp.Regexp](f func(), want StringOrRegexp) Checker {
	return &panicMatchesChecker{
		got:   f,
		want:  want,
		match: newMatcher(want),
	}
}

type panicMatchesChecker struct {
	got   func()
	want  any
	match matcher
}

func (c *panicMatchesChecker) Check(note func(key string, value any)) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			err = errors.New("function did not panic")
			return
		}
		msg := fmt.Sprint(r)
		note("panic value", msg)
		err = c.match(msg, "panic value does not match regexp", note)
	}()
	c.got()
	return nil
}

func (c *panicMatchesChecker) Args() []Arg {
	return []Arg{{Name: "function", Value: c.got}, {Name: "regexp", Value: c.want}}
}

// IsNil returns a Checker checking that the provided value is equal to nil.
//
// Note that an interface value containing a nil concrete
// type is not considered to be nil.
func IsNil[T any](got T) Checker {
	return isNilChecker[T]{
		got: got,
	}
}

type isNilChecker[T any] struct {
	got T
}

func (c isNilChecker[T]) Check(note func(key string, value any)) error {
	v := reflect.ValueOf(&c.got).Elem()
	if !canBeNil(v.Kind()) {
		return BadCheckf("type %v can never be nil", v.Type())
	}
	if v.IsNil() {
		return nil
	}
	return errors.New("got non-nil value")
}

func (c isNilChecker[T]) Args() []Arg {
	return []Arg{{Name: "got", Value: c.got}}
}

func (c isNilChecker[T]) negatedError() error {
	v := reflect.ValueOf(c.got)
	if v.IsValid() {
		return fmt.Errorf("got nil %s but want non-nil", v.Kind())
	}
	return errors.New("got <nil> but want non-nil")
}

// IsNotNil returns a Checker checking that the provided value is not nil.
// IsNotNil(v) is the equivalent of qt.Not(qt.IsNil(v)).
func IsNotNil[T any](got T) Checker {
	return Not(IsNil(got))
}

// HasLen returns a Checker checking that the provided value has the given
// length. The value may be a slice, array, channel, map or string.
func HasLen[T any](got T, n int) Checker {
	return &hasLenChecker[T]{
		got:     got,
		wantLen: n,
	}
}

type hasLenChecker[T any] struct {
	got     T
	wantLen int
}

func (c *hasLenChecker[T]) Check(note func(key string, value any)) (err error) {
	// TODO we're deliberately not allowing HasLen(interfaceValue) here.
	// Perhaps we should?
	v := reflect.ValueOf(&c.got).Elem()
	switch v.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
	default:
		note("got", c.got)
		return BadCheckf("first argument of type %v has no length", v.Type())
	}
	length := v.Len()
	note("len(got)", length)
	if length != c.wantLen {
		return fmt.Errorf("unexpected length")
	}
	return nil
}

func (c *hasLenChecker[T]) Args() []Arg {
	return []Arg{{Name: "got", Value: c.got}, {Name: "want length", Value: c.wantLen}}
}

// Implements returns a Checker checking that the provided value implements the
// interface specified by the type parameter.
func Implements[I any](got any) Checker {
	return &implementsChecker{
		got:  got,
		want: typeOf[I](),
	}
}

type implementsChecker struct {
	got  any
	want reflect.Type
}

var emptyInterface = reflect.TypeOf((*any)(nil)).Elem()

func (c *implementsChecker) Check(note func(key string, value any)) (err error) {
	if c.got == nil {
		note("error", Unquoted("got nil value but want non-nil"))
		note("got", c.got)
		return ErrSilent
	}
	if c.want.Kind() != reflect.Interface {
		note("want interface", Unquoted(c.want.String()))
		return BadCheckf("want an interface type but a concrete type was provided")
	}

	gotType := reflect.TypeOf(c.got)
	if !gotType.Implements(c.want) {
		return fmt.Errorf("got value does not implement wanted interface")
	}

	return nil
}

func (c *implementsChecker) Args() []Arg {
	return []Arg{{Name: "got", Value: c.got}, {Name: "want interface", Value: Unquoted(c.want.String())}}

}

// Satisfies returns a Checker checking that the provided value, when used as
// argument of the provided predicate function, causes the function to return
// true.
func Satisfies[T any](got T, f func(T) bool) Checker {
	return &satisfiesChecker[T]{
		got:       got,
		predicate: f,
	}
}

type satisfiesChecker[T any] struct {
	got       T
	predicate func(T) bool
}

// Check implements Checker.Check by checking that args[0](got) == true.
func (c *satisfiesChecker[T]) Check(note func(key string, value any)) error {
	if c.predicate(c.got) {
		return nil
	}
	return fmt.Errorf("value does not satisfy predicate function")
}

func (c *satisfiesChecker[T]) Args() []Arg {
	return []Arg{{
		Name:  "got",
		Value: c.got,
	}, {
		Name:  "predicate",
		Value: c.predicate,
	}}
}

// IsTrue returns a Checker checking that the provided value is true.
func IsTrue[T ~bool](got T) Checker {
	return Equals(got, true)
}

// IsFalse returns a Checker checking that the provided value is false.
func IsFalse[T ~bool](got T) Checker {
	return Equals(got, false)
}

// Not returns a Checker negating the given Checker.
func Not(c Checker) Checker {
	// Not(Not(c)) becomes c.
	if c, ok := c.(notChecker); ok {
		return c.Checker
	}
	return notChecker{
		Checker: c,
	}
}

type notChecker struct {
	Checker
}

func (c notChecker) Check(note func(key string, value any)) error {
	err := c.Checker.Check(note)
	if IsBadCheck(err) {
		return err
	}
	if err != nil {
		return nil
	}
	if c, ok := c.Checker.(negatedError); ok {
		return c.negatedError()
	}
	return errors.New("unexpected success")
}

// StringContains returns a Checker checking that the given string contains the
// given substring.
func StringContains[T ~string](got, substr T) Checker {
	return &stringContainsChecker[T]{
		got:    got,
		substr: substr,
	}
}

type stringContainsChecker[T ~string] struct {
	got, substr T
}

func (c *stringContainsChecker[T]) Check(note func(key string, value any)) error {
	if strings.Contains(string(c.got), string(c.substr)) {
		return nil
	}
	return errors.New("no substring match found")
}

func (c *stringContainsChecker[T]) Args() []Arg {
	return []Arg{{
		Name:  "got",
		Value: c.got,
	}, {
		Name:  "substr",
		Value: c.substr,
	}}
}

// SliceContains returns a Checker that succeeds if the given
// slice contains the given element, by comparing for equality.
func SliceContains[T any](container []T, elem T) Checker {
	return SliceAny(container, F2(Equals[T], elem))
}

// MapContains returns a Checker that succeeds if the given value is
// contained in the values of the given map, by comparing for equality.
func MapContains[K comparable, V any](container map[K]V, elem V) Checker {
	return MapAny(container, F2(Equals[V], elem))
}

// SliceAny returns a Checker that uses the given checker to check elements
// of a slice. It succeeds if f(v) passes the check for any v in the slice.
//
// See the F2 function for a way to adapt a regular checker function
// to the type expected for the f argument here.
//
// See also SliceAll and SliceContains.
func SliceAny[T any](container []T, f func(elem T) Checker) Checker {
	return &anyChecker[T]{
		newIter: func() containerIter[T] {
			return newSliceIter(container)
		},
		container:   container,
		elemChecker: f,
	}
}

// MapAny returns a Checker that uses checkers returned by f to check values
// of a map. It succeeds if f(v) passes the check for any value v in the map.
//
// See the F2 function for a way to adapt a regular checker function
// to the type expected for the f argument here.
//
// See also MapAll and MapContains.
func MapAny[K comparable, V any](container map[K]V, f func(elem V) Checker) Checker {
	return &anyChecker[V]{
		newIter: func() containerIter[V] {
			return newMapIter(container)
		},
		container:   container,
		elemChecker: f,
	}
}

type anyChecker[T any] struct {
	newIter     func() containerIter[T]
	container   any
	elemChecker func(T) Checker
}

func (c *anyChecker[T]) Check(note func(key string, value any)) error {
	for iter := c.newIter(); iter.next(); {
		// For the time being, discard the notes added by the sub-checker,
		// because it's not clear what a good behavior would be.
		// Should we print all the failed check for all elements? If there's only
		// one element in the container, the answer is probably yes,
		// but let's leave it for now.
		checker := c.elemChecker(iter.value())
		err := checker.Check(
			func(key string, value any) {},
		)
		if err == nil {
			return nil
		}
		if IsBadCheck(err) {
			return BadCheckf("at %s: %v", iter.key(), err)
		}
	}
	return errors.New("no matching element found")
}

func (c *anyChecker[T]) Args() []Arg {
	// We haven't got an instance of the underlying checker,
	// so just make one by passing the zero value. In general
	// no checker should panic when being created regardless
	// of the actual arguments, so that should be OK.
	args := []Arg{{
		Name:  "container",
		Value: c.container,
	}}
	if eargs := c.elemChecker(*new(T)).Args(); len(eargs) > 0 {
		args = append(args, eargs[1:]...)
	}
	return args
}

// SliceAll returns a Checker that uses checkers returned by f
// to check elements of a slice. It succeeds if all elements
// of the slice pass the check.
// On failure it prints the error from the first index that failed.
func SliceAll[T any](container []T, f func(elem T) Checker) Checker {
	return &allChecker[T]{
		newIter: func() containerIter[T] {
			return newSliceIter(container)
		},
		container:   container,
		elemChecker: f,
	}
}

// MapAll returns a Checker that uses checkers returned by f to check values
// of a map. It succeeds if f(v) passes the check for all values v in the map.
func MapAll[K comparable, V any](container map[K]V, f func(elem V) Checker) Checker {
	return &allChecker[V]{
		newIter: func() containerIter[V] {
			return newMapIter(container)
		},
		container:   container,
		elemChecker: f,
	}
}

type allChecker[T any] struct {
	newIter     func() containerIter[T]
	container   any
	elemChecker func(T) Checker
}

func (c *allChecker[T]) Check(notef func(key string, value any)) error {
	for iter := c.newIter(); iter.next(); {
		// Store any notes added by the checker so
		// we can add our own note at the start
		// to say which element failed.
		var notes []note
		checker := c.elemChecker(iter.value())
		err := checker.Check(
			func(key string, val any) {
				notes = append(notes, note{key, val})
			},
		)
		if err == nil {
			continue
		}
		if IsBadCheck(err) {
			return BadCheckf("at %s: %v", iter.key(), err)
		}
		notef("error", Unquoted("mismatch at "+iter.key()))
		// TODO should we print the whole container value in
		// verbose mode?
		if err != ErrSilent {
			// If the error's not silent, the checker is expecting
			// the caller to print the error and the value that failed.
			notef("error", Unquoted(err.Error()))
			notef("first mismatched element", iter.value())
		}
		for _, n := range notes {
			notef(n.key, n.value)
		}
		return ErrSilent
	}
	return nil
}

func (c *allChecker[T]) Args() []Arg {
	// We haven't got an instance of the underlying checker,
	// so just make one by passing the zero value. In general
	// no checker should panic when being created regardless
	// of the actual arguments, so that should be OK.
	args := []Arg{{
		Name:  "container",
		Value: c.container,
	}}
	if eargs := c.elemChecker(*new(T)).Args(); len(eargs) > 0 {
		args = append(args, eargs[1:]...)
	}
	return args
}

// JSONEquals returns a Checker that checks whether a string or byte slice is
// JSON-equivalent to a Go value. See CodecEquals for more information.
//
// It uses DeepEquals to do the comparison. If a more sophisticated comparison
// is required, use CodecEquals directly.
func JSONEquals[T []byte | string](got T, want any) Checker {
	return CodecEquals(got, want, json.Marshal, json.Unmarshal)
}

// CodecEquals returns a Checker that checks for codec value equivalence.
//
// It expects two arguments: a byte slice or a string containing some
// codec-marshaled data, and a Go value.
//
// It uses unmarshal to unmarshal the data into an interface{} value.
// It marshals the Go value using marshal, then unmarshals the result into
// an any value.
//
// It then checks that the two interface{} values are deep-equal to one
// another, using CmpEquals(opts) to perform the check.
//
// See JSONEquals for an example of this in use.
func CodecEquals[T []byte | string](
	got T,
	want any,
	marshal func(any) ([]byte, error),
	unmarshal func([]byte, any) error,
	opts ...cmp.Option,
) Checker {
	return &codecEqualChecker[T]{
		argPair:   argPairOf(got, want),
		marshal:   marshal,
		unmarshal: unmarshal,
		opts:      opts,
	}
}

type codecEqualChecker[T []byte | string] struct {
	argPair[T, any]
	marshal   func(any) ([]byte, error)
	unmarshal func([]byte, any) error
	opts      []cmp.Option
}

func (c *codecEqualChecker[T]) Check(note func(key string, value any)) error {
	wantContentBytes, err := c.marshal(c.want)
	if err != nil {
		return BadCheckf("cannot marshal expected contents: %v", err)
	}
	var wantContentVal any
	if err := c.unmarshal(wantContentBytes, &wantContentVal); err != nil {
		return BadCheckf("cannot unmarshal expected contents: %v", err)
	}
	var gotContentVal any
	if err := c.unmarshal([]byte(c.got), &gotContentVal); err != nil {
		return fmt.Errorf("cannot unmarshal obtained contents: %v; %q", err, c.got)
	}
	cmpEq := CmpEquals(gotContentVal, wantContentVal, c.opts...).(*cmpEqualsChecker[any])
	return cmpEq.Check(note)
}

// ErrorAs retruns a Checker checking that the error is or wraps a specific
// error type. If so, it assigns it to the provided pointer. This is analogous
// to calling errors.As.
func ErrorAs[T any](got error, want *T) Checker {
	return &errorAsChecker[T]{
		got:  got,
		want: want,
	}
}

type errorAsChecker[T any] struct {
	got  error
	want *T
}

func (c *errorAsChecker[T]) Check(note func(key string, value any)) (err error) {
	if c.got == nil {
		return errors.New("got nil error but want non-nil")
	}
	gotErr := c.got.(error)
	defer func() {
		// A panic is raised when the target is not a pointer to an interface
		// or error.
		if r := recover(); r != nil {
			err = BadCheckf("%s", r)
		}
	}()
	want := c.want
	if want == nil {
		want = new(T)
	}
	if !errors.As(gotErr, want) {
		return errors.New("wanted type is not found in error chain")
	}
	return nil
}

func (c *errorAsChecker[T]) Args() []Arg {
	return []Arg{{
		Name:  "got",
		Value: c.got,
	}, {
		Name:  "as type",
		Value: Unquoted(typeOf[T]().String()),
	}}
}

// ErrorIs returns a Checker that checks that the error is or wraps a specific
// error value. This is analogous to calling errors.Is.
func ErrorIs(got, want error) Checker {
	return &errorIsChecker{
		argPair: argPairOf(got, want),
	}
}

type errorIsChecker struct {
	argPair[error, error]
}

func (c *errorIsChecker) Check(note func(key string, value any)) error {
	if c.got == nil && c.want != nil {
		return errors.New("got nil error but want non-nil")
	}
	if !errors.Is(c.got, c.want) {
		return errors.New("wanted error is not found in error chain")
	}
	return nil
}

type matcher = func(got string, msg string, note func(key string, value any)) error

// newMatcher returns a matcher function that can be used by checkers when
// checking that a string or an error matches the provided StringOrRegexp.
func newMatcher[StringOrRegexp string | *regexp.Regexp](regex StringOrRegexp) matcher {
	var re *regexp.Regexp
	switch r := any(regex).(type) {
	case string:
		re0, err := regexp.Compile("^(" + r + ")$")
		if err != nil {
			return func(got string, msg string, note func(key string, value any)) error {
				note("regexp", r)
				return BadCheckf("cannot compile regexp: %s", err)
			}
		}
		re = re0
	case *regexp.Regexp:
		re = r
	}
	return func(got string, msg string, note func(key string, value any)) error {
		if re.MatchString(got) {
			return nil
		}
		return errors.New(msg)
	}
}

func argPairOf[A, B any](a A, b B) argPair[A, B] {
	return argPair[A, B]{a, b}
}

type argPair[A, B any] struct {
	got  A
	want B
}

func (p argPair[A, B]) Args() []Arg {
	return []Arg{{
		Name:  "got",
		Value: p.got,
	}, {
		Name:  "want",
		Value: p.want,
	}}
}

// canBeNil reports whether a value or type of the given kind can be nil.
func canBeNil(k reflect.Kind) bool {
	switch k {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return true
	}
	return false
}

func typeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

func valueAs[T any](v reflect.Value) (r T) {
	reflect.ValueOf(&r).Elem().Set(v)
	return
}

// F2 factors a 2-argument checker function into a single argument function suitable
// for passing to an *Any or *All checker. Whenever the returned function is called,
// cf is called with arguments (got, want).
func F2[Got, Want any](cf func(got Got, want Want) Checker, want Want) func(got Got) Checker {
	return func(got Got) Checker {
		return cf(got, want)
	}
}
