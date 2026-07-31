// +build !windows

package ole

import "testing"

// TestComSetupAndShutDown tests that API fails on Linux.
func TestComSetupAndShutDown(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := coInitialize()
	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}

	CoUninitialize()
}

// TestComPublicSetupAndShutDown tests that API fails on Linux.
func TestComPublicSetupAndShutDown(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := CoInitialize(0)
	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}

	CoUninitialize()
}

// TestComPublicSetupAndShutDown_WithValue tests that API fails on Linux.
func TestComPublicSetupAndShutDown_WithValue(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := CoInitialize(5)
	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}

	CoUninitialize()
}

// TestComExSetupAndShutDown tests that API fails on Linux.
func TestComExSetupAndShutDown(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := coInitializeEx(COINIT_MULTITHREADED)
	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}

	CoUninitialize()
}

// TestComPublicExSetupAndShutDown tests that API fails on Linux.
func TestComPublicExSetupAndShutDown(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := CoInitializeEx(0, COINIT_MULTITHREADED)
	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}

	CoUninitialize()
}

// TestComPublicExSetupAndShutDown_WithValue tests that API fails on Linux.
func TestComPublicExSetupAndShutDown_WithValue(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	err := CoInitializeEx(5, COINIT_MULTITHREADED)
	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}

	CoUninitialize()
}

// TestClsidFromProgID_WindowsMediaNSSManager tests that API fails on Linux.
func TestClsidFromProgID_WindowsMediaNSSManager(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	coInitialize()
	defer CoUninitialize()
	_, err := CLSIDFromProgID("WMPNSSCI.NSSManager")
	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}
}

// TestClsidFromString_WindowsMediaNSSManager tests that API fails on Linux.
func TestClsidFromString_WindowsMediaNSSManager(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	coInitialize()
	defer CoUninitialize()
	_, err := CLSIDFromString("{92498132-4D1A-4297-9B78-9E2E4BA99C07}")

	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}
}

// TestCreateInstance_WindowsMediaNSSManager tests that API fails on Linux.
func TestCreateInstance_WindowsMediaNSSManager(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Log(r)
			t.Fail()
		}
	}()

	coInitialize()
	defer CoUninitialize()
	_, err := CLSIDFromProgID("WMPNSSCI.NSSManager")

	if err == nil {
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}
}

// TestError tests that API fails on Linux.
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
		t.Error("should be error, because only Windows is supported.")
		t.FailNow()
	}

	switch vt := err.(type) {
	case *OleError:
	default:
		t.Fatalf("should be *ole.OleError %t", vt)
	}
}
