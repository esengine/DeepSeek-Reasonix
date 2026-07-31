package u

// True is a `bool` that is set to true
var True = NewBool(true)

// False is a `bool` that is set to false
var False = NewBool(false)

// Bool is a `bool` that can be unset
type Bool = Var[bool]

// NewBool creates a new Bool with the given value
func NewBool(val bool) Bool {
	return NewVar(val)
}

// String is a `string` that can be unset
type String = Var[string]

// NewString creates a new String with the given value
func NewString(val string) String {
	return NewVar(val)
}

// Float64 is a `float64` that can be unset
type Float64 = Var[float64]

// NewFloat64 creates a new Float64 with the given value
func NewFloat64(val float64) Float64 {
	return NewVar(val)
}

// Float32 is a `float32` that can be unset
type Float32 = Var[float32]

// NewFloat32 creates a new Float32 with the given value
func NewFloat32(val float32) Float32 {
	return NewVar(val)
}

// Uint is a `uint` that can be unset
type Uint = Var[uint]

// NewUint creates a new Uint with the given value
func NewUint(val uint) Uint {
	return NewVar(val)
}

// Uint8 is a `uint8` that can be unset
type Uint8 = Var[uint8]

// NewUint8 creates a new Uint8 with the given value
func NewUint8(val uint8) Uint8 {
	return NewVar(val)
}

// Uint16 is a `uint16` that can be unset
type Uint16 = Var[uint16]

// NewUint16 creates a new Uint16 with the given value
func NewUint16(val uint16) Uint16 {
	return NewVar(val)
}

// Uint32 is a `uint32` that can be unset
type Uint32 = Var[uint32]

// NewUint32 creates a new Uint32 with the given value
func NewUint32(val uint32) Uint32 {
	return NewVar(val)
}

// Uint64 is a `uint64` that can be unset
type Uint64 = Var[uint64]

// NewUint64 creates a new Uint64 with the given value
func NewUint64(val uint64) Uint64 {
	return NewVar(val)
}

// Byte is a `byte` that can be unset
type Byte = Var[byte]

// NewByte creates a new Byte with the given value
func NewByte(val byte) Byte {
	return NewVar(val)
}

// Rune is a `rune` that can be unset
type Rune = Var[rune]

// NewRune creates a new Rune with the given value
func NewRune(val rune) Rune {
	return NewVar(val)
}

// Complex64 is a `complex64` that can be unset
type Complex64 = Var[complex64]

// NewComplex64 creates a new Complex64 with the given value
func NewComplex64(val complex64) Complex64 {
	return NewVar(val)
}

// Complex128 is a `complex128` that can be unset
type Complex128 = Var[complex128]

// NewComplex128 creates a new Complex128 with the given value
func NewComplex128(val complex128) Complex128 {
	return NewVar(val)
}

// Int is an `int` that can be unset
type Int = Var[int]

// NewInt creates a new Int with the given value
func NewInt(val int) Int {
	return NewVar(val)
}

// Int8 is an `int8` that can be unset
type Int8 = Var[int8]

// NewInt8 creates a new Int8 with the given value
func NewInt8(val int8) Int8 {
	return NewVar(val)
}

// Int16 is an `int16` that can be unset
type Int16 = Var[int16]

// NewInt16 creates a new Int16 with the given value
func NewInt16(val int16) Int16 {
	return NewVar(val)
}

// Int32 is an `int32` that can be unset
type Int32 = Var[int32]

// NewInt32 creates a new Int32 with the given value
func NewInt32(val int32) Int32 {
	return NewVar(val)
}

// Int64 is an `int64` that can be unset
type Int64 = Var[int64]

// NewInt64 creates a new Int64 with the given value
func NewInt64(val int64) Int64 {
	return NewVar(val)
}

// Var is a variable that can be set, unset and queried for its state.
type Var[T any] struct {
	val T
	set bool
}

// Get the value of the variable
// Returns the zero value if unset
func (v *Var[T]) Get() T {
	return v.val
}

// Set the value of the variable
func (v *Var[T]) Set(val T) {
	v.val = val
	v.set = true
}

// IsSet returns true when a value has been set
func (v *Var[T]) IsSet() bool {
	return v.set
}

// Unset resets the value to the zero value and sets the variable to "unset"
func (v *Var[T]) Unset() {
	v.set = false
	var temp T
	v.val = temp
}

// NewVar creates a new Var with the given value
func NewVar[T any](val T) Var[T] {
	return Var[T]{val: val, set: true}
}
