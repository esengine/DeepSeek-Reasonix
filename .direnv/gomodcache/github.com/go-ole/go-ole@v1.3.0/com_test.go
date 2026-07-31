// +build windows

package ole

import (
	"fmt"
	"testing"
)

func TestComSetupAndShutDown(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := coInitialize()
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	CoUninitialize()
}

func TestComPublicSetupAndShutDown(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := CoInitialize(0)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	CoUninitialize()
}

func TestComPublicSetupAndShutDown_WithValue(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := CoInitialize(5)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	CoUninitialize()
}

func TestComExSetupAndShutDown(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := coInitializeEx(COINIT_MULTITHREADED)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	CoUninitialize()
}

func TestComPublicExSetupAndShutDown(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := CoInitializeEx(0, COINIT_MULTITHREADED)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	CoUninitialize()
}

func TestComPublicExSetupAndShutDown_WithValue(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := CoInitializeEx(5, COINIT_MULTITHREADED)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	CoUninitialize()
}

func TestClsidFromProgID_WindowsMediaNSSManager(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	expected := &GUID{0x92498132, 0x4D1A, 0x4297, [8]byte{0x9B, 0x78, 0x9E, 0x2E, 0x4B, 0xA9, 0x9C, 0x07}}

	coInitialize()
	defer CoUninitialize()
	actual, err := CLSIDFromProgID("WMPNSSCI.NSSManager")
	if err == nil {
		if !IsEqualGUID(expected, actual) {
			t.Log(err)
			t.Log(fmt.Sprintf("Actual GUID: %+v\n", actual))
			t.Fail()
		}
	}
}

func TestClsidFromString_WindowsMediaNSSManager(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	expected := &GUID{0x92498132, 0x4D1A, 0x4297, [8]byte{0x9B, 0x78, 0x9E, 0x2E, 0x4B, 0xA9, 0x9C, 0x07}}

	coInitialize()
	defer CoUninitialize()
	actual, err := CLSIDFromString("{92498132-4D1A-4297-9B78-9E2E4BA99C07}")

	if !IsEqualGUID(expected, actual) {
		t.Log(err)
		t.Log(fmt.Sprintf("Actual GUID: %+v\n", actual))
		t.Fail()
	}
}

func TestCreateInstance_WindowsMediaNSSManager(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	expected := &GUID{0x92498132, 0x4D1A, 0x4297, [8]byte{0x9B, 0x78, 0x9E, 0x2E, 0x4B, 0xA9, 0x9C, 0x07}}

	coInitialize()
	defer CoUninitialize()
	actual, err := CLSIDFromProgID("WMPNSSCI.NSSManager")

	if err == nil {
		if !IsEqualGUID(expected, actual) {
			t.Log(err)
			t.Log(fmt.Sprintf("Actual GUID: %+v\n", actual))
			t.Fail()
		}

		unknown, err := CreateInstance(actual, IID_IUnknown)
		if err != nil {
			t.Log(err)
			t.Fail()
		}
		unknown.Release()
	}
}

func TestError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	coInitialize()
	defer CoUninitialize()
	_, err := CLSIDFromProgID("INTERFACE-NOT-FOUND")
	if err == nil {
		t.Fatalf("should be fail: %v", err)
	}

	switch vt := err.(type) {
	case *OleError:
	default:
		t.Fatalf("should be *ole.OleError %t", vt)
	}
}
