// +build windows

package ole

import (
	"fmt"
	"strings"
	"testing"
)

func Example_quickbooks() {
	var err error

	connection := &Connection{nil}

	err = connection.Initialize()
	if err != nil {
		return
	}
	defer connection.Uninitialize()

	err = connection.Create("QBXMLRP2.RequestProcessor.1")
	if err != nil {
		if err.(*OleError).Code() == CO_E_CLASSSTRING {
			return
		}
	}
	defer connection.Release()

	dispatch, err := connection.Dispatch()
	if err != nil {
		return
	}
	defer dispatch.Release()
}

func TestConnectHelperCallDispatch_QuickBooks(t *testing.T) {
	var err error

	connection := &Connection{nil}

	err = connection.Initialize()
	if err != nil {
		t.Log(err)
		t.FailNow()
	}
	defer connection.Uninitialize()

	err = connection.Create("QBXMLRP2.RequestProcessor.1")
	if err != nil {
		if err.(*OleError).Code() == CO_E_CLASSSTRING {
			return
		}
		t.Log(err)
		t.FailNow()
	}
	defer connection.Release()

	dispatch, err := connection.Dispatch()
	if err != nil {
		t.Log(err)
		t.FailNow()
	}
	defer dispatch.Release()

	var result *VARIANT

	_, err = dispatch.Call("OpenConnection2", "", "Test Application 1", 1)
	if err != nil {
		t.Log(err)
		t.FailNow()
	}

	result, err = dispatch.Call("BeginSession", "", 2)
	if err != nil {
		t.Log(err)
		t.FailNow()
	}

	ticket := result.ToString()

	_, err = dispatch.Call("EndSession", ticket)
	if err != nil {
		t.Log(err)
		t.Fail()
	}

	_, err = dispatch.Call("CloseConnection")
	if err != nil {
		t.Log(err)
		t.Fail()
	}
}

func TestConnectHelperDispatchProperty_QuickBooks(t *testing.T) {
	var err error

	connection := &Connection{nil}

	err = connection.Initialize()
	if err != nil {
		t.Log(err)
		t.FailNow()
	}
	defer connection.Uninitialize()

	err = connection.Create("QBXMLRP2.RequestProcessor.1")
	if err != nil {
		if err.(*OleError).Code() == CO_E_CLASSSTRING {
			return
		}
		t.Log(err)
		t.FailNow()
	}
	defer connection.Release()

	dispatch, err := connection.Dispatch()
	if err != nil {
		t.Log(err)
		t.FailNow()
	}
	defer dispatch.Release()

	var result *VARIANT

	_, err = dispatch.Call("OpenConnection2", "", "Test Application 1", 1)
	if err != nil {
		t.Log(err)
		t.FailNow()
	}

	result, err = dispatch.Call("BeginSession", "", 2)
	if err != nil {
		t.Log(err)
		t.FailNow()
	}

	ticket := result.ToString()

	result, err = dispatch.Get("QBXMLVersionsForSession", ticket)
	if err != nil {
		t.Log(err)
		t.FailNow()
	}

	conversion := result.ToArray()

	totalElements, _ := conversion.TotalElements(0)
	if totalElements != 13 {
		t.Log(fmt.Sprintf("%d total elements does not equal 13\n", totalElements))
		t.Fail()
	}

	versions := conversion.ToStringArray()
	expectedVersionString := "1.0, 1.1, 2.0, 2.1, 3.0, 4.0, 4.1, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0"
	versionString := strings.Join(versions, ", ")

	if len(versions) != 13 {
		t.Log(fmt.Sprintf("%s\n", versionString))
		t.Fail()
	}

	if expectedVersionString != versionString {
		t.Log(fmt.Sprintf("Expected: %s\nActual: %s", expectedVersionString, versionString))
		t.Fail()
	}

	conversion.Release()

	_, err = dispatch.Call("EndSession", ticket)
	if err != nil {
		t.Log(err)
		t.Fail()
	}

	_, err = dispatch.Call("CloseConnection")
	if err != nil {
		t.Log(err)
		t.Fail()
	}
}
