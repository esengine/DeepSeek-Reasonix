package slicer

import "testing"

func TestStringClear(t *testing.T) {
	data := []string{"cat", "bat", "dog", "arachnid"}
	s := String(data)

	expected := 4
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}

func TestInterfaceClear(t *testing.T) {

	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.Add(a)
	s.Add(b)

	expected := 2
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}
func TestInt32Clear(t *testing.T) {

	data := []int32{1, 2, 3}
	s := Int32(data)

	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}

func TestInt16Clear(t *testing.T) {

	data := []int16{1, 2, 3}
	s := Int16(data)

	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}

func TestInt8Clear(t *testing.T) {

	data := []int8{1, 2, 3}
	s := Int8(data)

	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}

func TestIntClear(t *testing.T) {

	data := []int{1, 2, 3}
	s := Int(data)

	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}

func TestBoolClear(t *testing.T) {

	data := []bool{true}
	s := Bool(data)

	expected := 1
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}

func TestFloat64Clear(t *testing.T) {

	data := []float64{1.4, 2.1, 3}
	s := Float64(data)

	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}
func TestFloat32Clear(t *testing.T) {

	data := []float32{1.4, 2.1, 3}
	s := Float32(data)

	expected := 3
	result := s.Length()
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
	s.Clear()
	if s.Length() != 0 {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}
