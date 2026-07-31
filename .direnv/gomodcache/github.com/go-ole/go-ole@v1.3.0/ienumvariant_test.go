// +build windows

package ole

import "testing"

func TestIEnumVariant_wmi(t *testing.T) {
	var err error
	var classID *GUID

	IID_ISWbemLocator := &GUID{0x76a6415b, 0xcb41, 0x11d1, [8]byte{0x8b, 0x02, 0x00, 0x60, 0x08, 0x06, 0xd9, 0xb6}}

	err = CoInitialize(0)
	if err != nil {
		t.Errorf("Initialize error: %v", err)
	}
	defer CoUninitialize()

	classID, err = ClassIDFrom("WbemScripting.SWbemLocator")
	if err != nil {
		t.Errorf("CreateObject WbemScripting.SWbemLocator returned with %v", err)
	}

	comserver, err := CreateInstance(classID, IID_IUnknown)
	if err != nil {
		t.Errorf("CreateInstance WbemScripting.SWbemLocator returned with %v", err)
	}
	if comserver == nil {
		t.Error("CreateObject WbemScripting.SWbemLocator not an object")
	}
	defer comserver.Release()

	dispatch, err := comserver.QueryInterface(IID_ISWbemLocator)
	if err != nil {
		t.Errorf("context.iunknown.QueryInterface returned with %v", err)
	}
	defer dispatch.Release()

	wbemServices, err := dispatch.CallMethod("ConnectServer")
	if err != nil {
		t.Errorf("ConnectServer failed with %v", err)
	}
	defer wbemServices.Clear()

	objectset, err := wbemServices.ToIDispatch().CallMethod("ExecQuery", "SELECT * FROM WIN32_Process")
	if err != nil {
		t.Errorf("ExecQuery failed with %v", err)
	}
	defer objectset.Clear()

	enum_property, err := objectset.ToIDispatch().GetProperty("_NewEnum")
	if err != nil {
		t.Errorf("Get _NewEnum property failed with %v", err)
	}
	defer enum_property.Clear()

	enum, err := enum_property.ToIUnknown().IEnumVARIANT(IID_IEnumVariant)
	if err != nil {
		t.Errorf("IEnumVARIANT() returned with %v", err)
	}
	if enum == nil {
		t.Error("Enum is nil")
		t.FailNow()
	}
	defer enum.Release()

	for tmp, length, err := enum.Next(1); length > 0; tmp, length, err = enum.Next(1) {
		if err != nil {
			t.Errorf("Next() returned with %v", err)
		}
		tmp_dispatch := tmp.ToIDispatch()
		defer tmp_dispatch.Release()

		props, err := tmp_dispatch.GetProperty("Properties_")
		if err != nil {
			t.Errorf("Get Properties_ property failed with %v", err)
		}
		defer props.Clear()

		props_enum_property, err := props.ToIDispatch().GetProperty("_NewEnum")
		if err != nil {
			t.Errorf("Get _NewEnum property failed with %v", err)
		}
		defer props_enum_property.Clear()

		props_enum, err := props_enum_property.ToIUnknown().IEnumVARIANT(IID_IEnumVariant)
		if err != nil {
			t.Errorf("IEnumVARIANT failed with %v", err)
		}
		defer props_enum.Release()

		class_variant, err := tmp_dispatch.GetProperty("Name")
		if err != nil {
			t.Errorf("Get Name property failed with %v", err)
		}
		defer class_variant.Clear()

		class_name := class_variant.ToString()
		t.Logf("Got %v", class_name)
	}
}
