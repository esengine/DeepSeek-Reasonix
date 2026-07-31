# qt: quicker Go tests

`go get github.com/go-quicktest/qt`

Package qt provides a collection of Go helpers for writing tests. It uses generics,
so requires Go 1.18 at least.

For a complete API reference, see the [package documentation](https://pkg.go.dev/github.com/go-quicktest/qt).

Quicktest helpers can be easily integrated inside regular Go tests, for
instance:
```go
    import "github.com/go-quicktest/qt"

    func TestFoo(t *testing.T) {
        t.Run("numbers", func(t *testing.T) {
            numbers, err := somepackage.Numbers()
            qt.Assert(t, qt.DeepEquals(numbers, []int{42, 47})
            qt.Assert(t, qt.ErrorMatches(err, "bad wolf"))
        })
        t.Run("nil", func(t *testing.T) {
            got := somepackage.MaybeNil()
            qt.Assert(t, qt.IsNil(got), qt.Commentf("value: %v", somepackage.Value))
        })
    }
```

### Assertions

An assertion looks like this, where `qt.Equals` could be replaced by any available
checker. If the assertion fails, the underlying `t.Fatal` method is called to
describe the error and abort the test.

    qt.Assert(t, qt.Equals(someValue, wantValue))

If you don’t want to abort on failure, use `Check` instead, which calls `Error`
instead of `Fatal`:

    qt.Check(t, qt.Equals(someValue, wantValue))

The library provides some base checkers like `Equals`, `DeepEquals`, `Matches`,
`ErrorMatches`, `IsNil` and others. More can be added by implementing the Checker
interface.

### Other helpers

The `Patch` helper makes it a little more convenient to change a global
or other variable for the duration of a test.
