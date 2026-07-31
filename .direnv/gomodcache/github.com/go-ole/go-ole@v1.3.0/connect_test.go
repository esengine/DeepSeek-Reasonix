// +build !windows

package ole

import "strings"

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

func Example_quickbooksConnectHelperCallDispatch() {
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
		return
	}
	defer connection.Release()

	dispatch, err := connection.Dispatch()
	if err != nil {
		return
	}
	defer dispatch.Release()

	var result *VARIANT

	_, err = dispatch.Call("OpenConnection2", "", "Test Application 1", 1)
	if err != nil {
		return
	}

	result, err = dispatch.Call("BeginSession", "", 2)
	if err != nil {
		return
	}

	ticket := result.ToString()

	_, err = dispatch.Call("EndSession", ticket)
	if err != nil {
		return
	}

	_, err = dispatch.Call("CloseConnection")
	if err != nil {
		return
	}
}

func Example_quickbooksConnectHelperDispatchProperty() {
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
		return
	}
	defer connection.Release()

	dispatch, err := connection.Dispatch()
	if err != nil {
		return
	}
	defer dispatch.Release()

	var result *VARIANT

	_, err = dispatch.Call("OpenConnection2", "", "Test Application 1", 1)
	if err != nil {
		return
	}

	result, err = dispatch.Call("BeginSession", "", 2)
	if err != nil {
		return
	}

	ticket := result.ToString()

	result, err = dispatch.Get("QBXMLVersionsForSession", ticket)
	if err != nil {
		return
	}

	conversion := result.ToArray()

	totalElements, _ := conversion.TotalElements(0)
	if totalElements != 13 {
		return
	}

	versions := conversion.ToStringArray()
	expectedVersionString := "1.0, 1.1, 2.0, 2.1, 3.0, 4.0, 4.1, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0"
	versionString := strings.Join(versions, ", ")

	if len(versions) != 13 {
		return
	}

	if expectedVersionString != versionString {
		return
	}

	conversion.Release()

	_, err = dispatch.Call("EndSession", ticket)
	if err != nil {
		return
	}

	_, err = dispatch.Call("CloseConnection")
	if err != nil {
		return
	}
}
