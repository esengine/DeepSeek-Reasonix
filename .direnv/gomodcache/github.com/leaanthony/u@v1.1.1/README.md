# u

u provides a simple way to create variables that are "unset" by default.

u is dependency free and has 100% test coverage.

## Why?

Go's default values are great most of the time, but sometimes you want to know if a value has been set or not. This is especially true when you want to know if a value has been set to its zero value or not.

For example, let's say you had a preferences struct like this:

```go
type Preferences struct {
    UseFeatureX bool
    Threshold int
}

func processPreferences(prefs Preferences) {
    // Uh oh...we're in a heap of trouble here...
    thirdPartyLibrary.EnableFeatureX(prefs.UseFeatureX)
    thirdPartyLibrary.SetThreshold(prefs.Threshold)
}

func main() {
    var prefs Preferences
    processPreferences(prefs)
}
```

There is no real way to know if `UseFeatureX` or `Threshold` have been set or not.

Using this library we can *eliminate* this problem:

```go
type Preferences struct {
    UseFeatureX u.Bool
    Threshold u.Int
}

func processPreferences(prefs Preferences) {
    // #winning
    if prefs.UseFeatureX.IsSet() {
        thirdPartyLibrary.EnableFeatureX(prefs.UseFeatureX.Get())
    }
    if prefs.Threshold.IsSet() {
        thirdPartyLibrary.SetThreshold(prefs.Threshold.Get())
    }
}

func main() {
    var prefs Preferences
    processPreferences(prefs)
}

```

## Why should any of this matter? 

It matters when you are working with third party libraries or frameworks that have default values that may not be the same as the Zero Go values. 
Perhaps the default value for a preference is `true` and you only want to set it if an explicit value has been specified.

## Usage

### Basic Usage

```go
var myVar u.Int

// Set the value
myVar.Set(10)

// Get the value
fmt.Println(myVar.Get()) // 10

// Check if the value has been set
fmt.Println(myVar.IsSet()) // true

// Unset the value
myVar.Unset()

// Check if the value has been set
fmt.Println(myVar.IsSet()) // false
```

### Structs

`New` methods are provided for setting values in structs.

```go
    type MyStruct struct {
        MyVar u.Int
        MyOption u.Bool
    }
	
    myStruct := MyStruct{
        MyVar: u.NewInt(42),
        MyOption: u.True,
    }
```

## Why not use pointer values?

Yes, that's one way you can solve this issue. Personally, I try to avoid having pointer values as it increases the chance of dereferencing errors. It also feels like a hacky approach to the problem which is one missed test away from a runtime error. 

## Supported types

- `u.Bool`
- `u.Int`
- `u.Int8`
- `u.Int16`
- `u.Int32`
- `u.Int64`
- `u.Uint`
- `u.Uint8`
- `u.Uint16`
- `u.Uint32`
- `u.Uint64`
- `u.Float32`
- `u.Float64`
- `u.Complex64`
- `u.Complex128`
- `u.String`
- `u.Byte`
- `u.Rune`

## Values

- `u.True`
- `u.False`

## Custom types

Any type can be used with this library by creating a `u.Var` of that type. For example:

```go

type MyCustomType struct {
    value string
}

var myVar u.Var[MyCustomType]

// Set the value
myVar.Set(MyCustomType{value: "hello"})
```

## License

MIT
