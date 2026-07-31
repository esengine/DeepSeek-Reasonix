package u_test

import (
	"github.com/leaanthony/u"
	"testing"
)

func TestVar_String(t *testing.T) {
	var v u.String
	if v.Get() != "" {
		t.Errorf("Get() = %v, want \"\"", v.Get())
	}

	v.Set("hello")
	if v.Get() != "hello" {
		t.Errorf("Get() = %v, want \"hello\"", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != "" {
		t.Errorf("Get() = %v, want \"\"", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Bool(t *testing.T) {

	var v u.Bool
	if v.Get() != false {
		t.Errorf("Get() = %v, want false", v.Get())
	}

	v.Set(true)
	if v.Get() != true {
		t.Errorf("Get() = %v, want true", v.Get())
	}

	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != false {
		t.Errorf("Get() = %v, want false", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Float64(t *testing.T) {

	var v u.Float64
	if v.Get() != 0.0 {
		t.Errorf("Get() = %v, want 0.0", v.Get())
	}

	v.Set(42.0)
	if v.Get() != 42.0 {
		t.Errorf("Get() = %v, want 42.0", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0.0 {
		t.Errorf("Get() = %v, want 0.0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Float32(t *testing.T) {

	var v u.Float32
	if v.Get() != 0.0 {
		t.Errorf("Get() = %v, want 0.0", v.Get())
	}

	v.Set(42.0)
	if v.Get() != 42.0 {
		t.Errorf("Get() = %v, want 42.0", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0.0 {
		t.Errorf("Get() = %v, want 0.0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Byte(t *testing.T) {

	var v u.Byte
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Rune(t *testing.T) {

	var v u.Rune
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set('x')
	if v.Get() != 'x' {
		t.Errorf("Get() = %v, want 'x'", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Complex64(t *testing.T) {

	var v u.Complex64
	if v.Get() != 0+0i {
		t.Errorf("Get() = %v, want 0+0i", v.Get())
	}

	v.Set(42 + 42i)
	if v.Get() != 42+42i {
		t.Errorf("Get() = %v, want 42+42i", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0+0i {
		t.Errorf("Get() = %v, want 0+0i", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Complex128(t *testing.T) {

	var v u.Complex128
	if v.Get() != 0+0i {
		t.Errorf("Get() = %v, want 0+0i", v.Get())
	}

	v.Set(42 + 42i)
	if v.Get() != 42+42i {
		t.Errorf("Get() = %v, want 42+42i", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0+0i {
		t.Errorf("Get() = %v, want 0+0i", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Int(t *testing.T) {

	var v u.Int
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Int8(t *testing.T) {

	var v u.Int8
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Int16(t *testing.T) {

	var v u.Int16
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Int32(t *testing.T) {

	var v u.Int32
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Int64(t *testing.T) {

	var v u.Int64
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Uint(t *testing.T) {

	var v u.Uint
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Uint8(t *testing.T) {

	var v u.Uint8
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Uint16(t *testing.T) {

	var v u.Uint16
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Uint32(t *testing.T) {

	var v u.Uint32
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Uint64(t *testing.T) {

	var v u.Uint64
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}

	v.Set(42)
	if v.Get() != 42 {
		t.Errorf("Get() = %v, want 42", v.Get())
	}
	if v.IsSet() != true {
		t.Errorf("IsSet() = %v, want true", v.IsSet())
	}

	v.Unset()
	if v.Get() != 0 {
		t.Errorf("Get() = %v, want 0", v.Get())
	}
	if v.IsSet() != false {
		t.Errorf("IsSet() = %v, want false", v.IsSet())
	}
}

func TestVar_Struct(t *testing.T) {

	type Preferences struct {
		Value1  u.Int
		Value2  u.Int8
		Value3  u.Int16
		Value4  u.Int32
		Value5  u.Int64
		Value6  u.Uint
		Value7  u.Uint8
		Value8  u.Uint16
		Value9  u.Uint32
		Value10 u.Uint64
		Value11 u.Float32
		Value12 u.Float64
		Value13 u.Byte
		Value14 u.Rune
		Value15 u.Complex64
		Value16 u.Complex128
		Value17 u.Bool
		Value18 u.Bool
		Value19 u.String
	}

	prefs := Preferences{
		Value1:  u.NewInt(42),
		Value2:  u.NewInt8(42),
		Value3:  u.NewInt16(42),
		Value4:  u.NewInt32(42),
		Value5:  u.NewInt64(42),
		Value6:  u.NewUint(42),
		Value7:  u.NewUint8(42),
		Value8:  u.NewUint16(42),
		Value9:  u.NewUint32(42),
		Value10: u.NewUint64(42),
		Value11: u.NewFloat32(42),
		Value12: u.NewFloat64(42),
		Value13: u.NewByte(42),
		Value14: u.NewRune('x'),
		Value15: u.NewComplex64(42 + 42i),
		Value16: u.NewComplex128(42 + 42i),
		Value17: u.True,
		Value18: u.False,
		Value19: u.NewString("hello"),
	}

	if prefs.Value1.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value1.Get())
	}
	if prefs.Value2.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value2.Get())
	}
	if prefs.Value3.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value3.Get())
	}
	if prefs.Value4.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value4.Get())
	}
	if prefs.Value5.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value5.Get())
	}
	if prefs.Value6.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value6.Get())
	}
	if prefs.Value7.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value7.Get())
	}
	if prefs.Value8.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value8.Get())
	}
	if prefs.Value9.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value9.Get())
	}
	if prefs.Value10.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value10.Get())
	}
	if prefs.Value11.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value11.Get())
	}
	if prefs.Value12.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value12.Get())
	}
	if prefs.Value13.Get() != 42 {
		t.Errorf("Get() = %v, want 42", prefs.Value13.Get())
	}
	if prefs.Value14.Get() != 'x' {
		t.Errorf("Get() = %v, want 'x'", prefs.Value14.Get())
	}
	if prefs.Value15.Get() != 42+42i {
		t.Errorf("Get() = %v, want 42+42i", prefs.Value15.Get())
	}
	if prefs.Value16.Get() != 42+42i {
		t.Errorf("Get() = %v, want 42+42i", prefs.Value16.Get())
	}
	if prefs.Value17.Get() != true {
		t.Errorf("Get() = %v, want true", prefs.Value17.Get())
	}
	if prefs.Value18.Get() != false {
		t.Errorf("Get() = %v, want false", prefs.Value18.Get())
	}
	if prefs.Value19.Get() != "hello" {
		t.Errorf("Get() = %v, want \"hello\"", prefs.Value19.Get())
	}
}
