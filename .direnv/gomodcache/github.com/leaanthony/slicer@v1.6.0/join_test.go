package slicer

import "testing"

func TestStringJoin(t *testing.T) {

	/* Test multiple elements */
	data := []string{"cat", "bat", "dog", "arachnid"}
	s := String(data)

	// Positive test
	expected := "cat,bat,dog,arachnid"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "cat,bat,dog,horse"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
	/* Test single element */
	s = String()
	s.Add("hello")

	// Positive test
	expected = "hello"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "hello,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = String()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

func TestInterfaceJoin(t *testing.T) {

	/* Test multiple elements */
	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.Add(a)
	s.Add(b)

	// Positive test
	expected := "1,hello"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,hello,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test single element */
	s = Interface()
	s.Add(a)

	// Positive test
	expected = "1"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Interface()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

}

func TestInt64Join(t *testing.T) {

	/* Test multiple elements */

	data := []int64{1, 2, 3}
	s := Int64(data)

	// Positive test
	expected := "1,2,3"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,2,3,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test single element */
	s = Int64()
	s.Add(1)

	// Positive test
	expected = "1"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Int64()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

func TestInt32Join(t *testing.T) {

	/* Test multiple elements */

	data := []int32{1, 2, 3}
	s := Int32(data)

	// Positive test
	expected := "1,2,3"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,2,3,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test single element */
	s = Int32()
	s.Add(1)

	// Positive test
	expected = "1"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Int32()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

func TestInt16Join(t *testing.T) {

	/* Test multiple elements */

	data := []int16{1, 2, 3}
	s := Int16(data)

	// Positive test
	expected := "1,2,3"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,2,3,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test single element */
	s = Int16()
	s.Add(1)

	// Positive test
	expected = "1"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Int16()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

func TestInt8Join(t *testing.T) {

	/* Test multiple elements */

	data := []int8{1, 2, 3}
	s := Int8(data)

	// Positive test
	expected := "1,2,3"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,2,3,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test single element */
	s = Int8()
	s.Add(1)

	// Positive test
	expected = "1"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Int8()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

func TestIntJoin(t *testing.T) {

	/* Test multiple elements */

	data := []int{1, 2, 3}
	s := Int(data)

	// Positive test
	expected := "1,2,3"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,2,3,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test single element */
	s = Int()
	s.Add(1)

	// Positive test
	expected = "1"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Int()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

func TestBoolJoin(t *testing.T) {

	/* Test multpile elements */
	data := []bool{true, false, true}
	s := Bool(data)

	// Positive test
	expected := "true,false,true"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "true,false,true,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
	/* Test single element */
	s = Bool()
	s.Add(false)

	// Positive test
	expected = "false"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "false,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Bool()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

func TestFloat64Join(t *testing.T) {

	data := []float64{1.4, 2.1, 3}
	s := Float64(data)

	// Positive test
	expected := "1.4,2.1,3"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1.4,2.1,3,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
	/* Test single element */
	s = Float64()
	s.Add(1.982)

	// Positive test
	expected = "1.982"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1.982,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Float64()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
func TestFloat32Join(t *testing.T) {

	data := []float32{1.4, 2.1, 3}
	s := Float32(data)

	// Positive test
	expected := "1.4,2.1,3"
	result := s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1.4,2.1,3,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test single element */
	s = Float32()
	s.Add(1.982)

	// Positive test
	expected = "1.982"
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	// Negative test
	expected = "1.982,"
	result = s.Join(",")
	if result == expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}

	/* Test no element */
	s = Float32()
	expected = ""
	result = s.Join(",")
	if result != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
