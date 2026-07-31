package slicer

import "testing"

func TestStringContains(t *testing.T) {
	data := []string{"cat", "bat", "dog", "arachnid"}
	s := String(data)

	// Positive test
	expected := true
	result := s.Contains("cat")
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	result = s.Contains("horse")
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}

func TestInterfaceContains(t *testing.T) {

	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.Add(a)
	s.Add(b)

	// Positive test
	expected := true
	result := s.Contains(a)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	var dummy interface{} = 5
	result = s.Contains(dummy)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}

func TestInt64Contains(t *testing.T) {

	data := []int64{1, 2, 3}
	s := Int64(data)

	// Positive test
	expected := true
	result := s.Contains(2)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	var badValue int64 = 9
	result = s.Contains(badValue)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}
func TestInt32Contains(t *testing.T) {

	data := []int32{1, 2, 3}
	s := Int32(data)

	// Positive test
	expected := true
	result := s.Contains(2)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	var badValue int32 = 9
	result = s.Contains(badValue)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}

func TestInt16Contains(t *testing.T) {

	data := []int16{1, 2, 3}
	s := Int16(data)

	// Positive test
	expected := true
	result := s.Contains(2)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	var badValue int16 = 9
	result = s.Contains(badValue)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}

func TestInt8Contains(t *testing.T) {

	data := []int8{1, 2, 3}
	s := Int8(data)

	// Positive test
	expected := true
	result := s.Contains(2)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	var badValue int8 = 9
	result = s.Contains(badValue)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}

func TestIntContains(t *testing.T) {

	data := []int{1, 2, 3}
	s := Int(data)

	// Positive test
	expected := true
	result := s.Contains(2)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	var badValue int = 9
	result = s.Contains(badValue)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}

func TestBoolContains(t *testing.T) {

	data := []bool{true}
	s := Bool(data)

	// Positive test
	expected := true
	result := s.Contains(true)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	result = s.Contains(false)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}

func TestFloat64Contains(t *testing.T) {

	data := []float64{1.4, 2.1, 3}
	s := Float64(data)

	// Positive test
	expected := true
	result := s.Contains(2.1)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	result = s.Contains(9.43)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}
func TestFloat32Contains(t *testing.T) {

	data := []float32{1.4, 2.1, 3}
	s := Float32(data)

	// Positive test
	expected := true
	result := s.Contains(2.1)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}

	// Negative test
	expected = false
	result = s.Contains(9.43)
	if result != expected {
		t.Errorf("Expected '%t', but got '%t'", expected, result)
	}
}
