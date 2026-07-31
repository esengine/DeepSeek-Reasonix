// +build windows

package main

import (
	"fmt"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

func main() {
	ole.CoInitialize(0)
	unknown, _ := oleutil.CreateObject("Outlook.Application")
	outlook, _ := unknown.QueryInterface(ole.IID_IDispatch)
	ns := oleutil.MustCallMethod(outlook, "GetNamespace", "MAPI").ToIDispatch()
	folder := oleutil.MustCallMethod(ns, "GetDefaultFolder", 10).ToIDispatch()
	contacts := oleutil.MustCallMethod(folder, "Items").ToIDispatch()
	count := oleutil.MustGetProperty(contacts, "Count").Value().(int32)
	for i := 1; i <= int(count); i++ {
		item, err := oleutil.GetProperty(contacts, "Item", i)
		if err == nil && item.VT == ole.VT_DISPATCH {
			if value, err := oleutil.GetProperty(item.ToIDispatch(), "FullName"); err == nil {
				fmt.Println(value.Value())
			}
		}
	}
	oleutil.MustCallMethod(outlook, "Quit")
}
