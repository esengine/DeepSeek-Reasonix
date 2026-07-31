package slicer

import "testing"

func TestStringLength(t *testing.T) {

	/* Test multiple elements */
	data := []string{"cat", "bat", "dog", "arachnid"}
	s := String(data)

	// Positive test
	expected := 4
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 3
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = String()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

func TestInterfaceLength(t *testing.T) {

	/* Test multiple elements */
	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.Add(a)
	s.Add(b)

	// Positive test
	expected := 2
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 3
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Interface()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

}

func TestInt64Length(t *testing.T) {

	/* Test multiple elements */

	data := []int64{1, 2, 3}
	s := Int64(data)

	// Positive test
	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 2
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Int64()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

func TestInt32Length(t *testing.T) {

	/* Test multiple elements */

	data := []int32{1, 2, 3}
	s := Int32(data)

	// Positive test
	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 2
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Int32()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

func TestInt16Length(t *testing.T) {

	/* Test multiple elements */

	data := []int16{1, 2, 3}
	s := Int16(data)

	// Positive test
	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 2
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Int16()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

func TestInt8Length(t *testing.T) {

	/* Test multiple elements */

	data := []int8{1, 2, 3}
	s := Int8(data)

	// Positive test
	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 2
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Int8()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

func TestIntLength(t *testing.T) {

	/* Test multiple elements */

	data := []int{1, 2, 3}
	s := Int(data)

	// Positive test
	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 2
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Int()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

func TestBoolLength(t *testing.T) {

	/* Test multpile elements */
	data := []bool{true, false, true}
	s := Bool(data)

	// Positive test
	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 2
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Bool()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

func TestFloat64Length(t *testing.T) {

	data := []float64{1.4, 2.1, 3}
	s := Float64(data)

	// Positive test
	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 2
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Float64()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}
func TestFloat32Length(t *testing.T) {

	data := []float32{1.4, 2.1, 3}
	s := Float32(data)

	// Positive test
	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	// Negative test
	expected = 2
	result = s.Length()
	if result == expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}

	/* Test no element */
	s = Float32()
	expected = 0
	result = s.Length()
	if result != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}
