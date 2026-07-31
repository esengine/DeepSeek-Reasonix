// +build windows

package main

import (
	"fmt"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

func main() {
	ole.CoInitialize(0)
	unknown, _ := oleutil.CreateObject("Microsoft.XMLHTTP")
	xmlhttp, _ := unknown.QueryInterface(ole.IID_IDispatch)
	_, err := oleutil.CallMethod(xmlhttp, "open", "GET", "http://rss.slashdot.org/Slashdot/slashdot", false)
	if err != nil {
		panic(err.Error())
	}
	_, err = oleutil.CallMethod(xmlhttp, "send", nil)
	if err != nil {
		panic(err.Error())
	}
	state := -1
	for state != 4 {
		state = int(oleutil.MustGetProperty(xmlhttp, "readyState").Val)
		time.Sleep(10000000)
	}
	responseXml := oleutil.MustGetProperty(xmlhttp, "responseXml").ToIDispatch()
	items := oleutil.MustCallMethod(responseXml, "selectNodes", "/rdf:RDF/item").ToIDispatch()
	length := int(oleutil.MustGetProperty(items, "length").Val)

	println(length)
	for n := 0; n < length; n++ {
		item := oleutil.MustGetProperty(items, "item", n).ToIDispatch()

		title := oleutil.MustCallMethod(item, "selectSingleNode", "title").ToIDispatch()
		fmt.Println(oleutil.MustGetProperty(title, "text").ToString())

		link := oleutil.MustCallMethod(item, "selectSingleNode", "link").ToIDispatch()
		fmt.Println("  " + oleutil.MustGetProperty(link, "text").ToString())

		title.Release()
		link.Release()
		item.Release()
	}
	items.Release()
	xmlhttp.Release()
}
