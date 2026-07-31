// +build windows

/*
	Demonstrates basic LibreOffce (OpenOffice) automation with OLE using GO-OLE.
	Usage: 	cd [...]\go-ole\example\libreoffice
			go run libreoffice.go
	References:
			http://www.openoffice.org/api/basic/man/tutorial/tutorial.pdf
			http://api.libreoffice.org/examples/examples.html#OLE_examples
			https://wiki.openoffice.org/wiki/Documentation/BASIC_Guide

	Tested environment:
			go 1.6.2 (windows/amd64)
			LibreOffice 5.1.0.3 (32 bit)
			Windows 10 (64 bit)

	The MIT License (MIT)
	Copyright (c) 2016 Sebastian Schleemilch <https://github.com/itschleemilch>.

	Permission is hereby granted, free of charge, to any person obtaining a copy of
	this software and associated documentation files (the "Software"), to deal in
	the Software without restriction, including without limitation the rights to use,
	copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software,
	and to permit persons to whom the Software is furnished to do so, subject to the
	following conditions:

	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED,
	INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR
	PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
	LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
	TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE
	OR OTHER DEALINGS IN THE SOFTWARE.
*/

package main

import (
	"fmt"
	"log"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

func checkError(err error, msg string) {
	if err != nil {
		log.Fatal(msg)
	}
}

// LOGetCell returns an handle to a cell within a worksheet
// LibreOffice Basic: GetCell = oSheet.getCellByPosition (nColumn , nRow)
func LOGetCell(worksheet *ole.IDispatch, nColumn int, nRow int) (cell *ole.IDispatch) {
	return oleutil.MustCallMethod(worksheet, "getCellByPosition", nColumn, nRow).ToIDispatch()
}

// LOGetCellRangeByName returns a named range (e.g. "A1:B4")
func LOGetCellRangeByName(worksheet *ole.IDispatch, rangeName string) (cells *ole.IDispatch) {
	return oleutil.MustCallMethod(worksheet, "getCellRangeByName", rangeName).ToIDispatch()
}

// LOGetCellString returns the displayed value
func LOGetCellString(cell *ole.IDispatch) (value string) {
	return oleutil.MustGetProperty(cell, "string").ToString()
}

// LOGetCellValue returns the cell's internal value (not formatted, dummy code, FIXME)
func LOGetCellValue(cell *ole.IDispatch) (value string) {
	val := oleutil.MustGetProperty(cell, "value")
	fmt.Printf("Cell: %+v\n", val)
	return val.ToString()
}

// LOGetCellError returns the error value of a cell (dummy code, FIXME)
func LOGetCellError(cell *ole.IDispatch) (result *ole.VARIANT) {
	return oleutil.MustGetProperty(cell, "error")
}

// LOSetCellString sets the text value of a cell
func LOSetCellString(cell *ole.IDispatch, text string) {
	oleutil.MustPutProperty(cell, "string", text)
}

// LOSetCellValue sets the numeric value of a cell
func LOSetCellValue(cell *ole.IDispatch, value float64) {
	oleutil.MustPutProperty(cell, "value", value)
}

// LOSetCellFormula sets the formula (in englisch language)
func LOSetCellFormula(cell *ole.IDispatch, formula string) {
	oleutil.MustPutProperty(cell, "formula", formula)
}

// LOSetCellFormulaLocal sets the formula in the user's language (e.g. German =SUMME instead of =SUM)
func LOSetCellFormulaLocal(cell *ole.IDispatch, formula string) {
	oleutil.MustPutProperty(cell, "FormulaLocal", formula)
}

// LONewSpreadsheet creates a new spreadsheet in a new window and returns a document handle.
func LONewSpreadsheet(desktop *ole.IDispatch) (document *ole.IDispatch) {
	var args = []string{}
	document = oleutil.MustCallMethod(desktop,
		"loadComponentFromURL", "private:factory/scalc", // alternative: private:factory/swriter
		"_blank", 0, args).ToIDispatch()
	return
}

// LOOpenFile opens a file (text, spreadsheet, ...) in a new window and returns a document
// handle. Example: /home/testuser/spreadsheet.ods
func LOOpenFile(desktop *ole.IDispatch, fullpath string) (document *ole.IDispatch) {
	var args = []string{}
	document = oleutil.MustCallMethod(desktop,
		"loadComponentFromURL", "file://"+fullpath,
		"_blank", 0, args).ToIDispatch()
	return
}

// LOSaveFile saves the current document.
// Only works if a file already exists,
// see https://wiki.openoffice.org/wiki/Saving_a_document
func LOSaveFile(document *ole.IDispatch) {
	// use storeAsURL if neccessary with third URL parameter
	oleutil.MustCallMethod(document, "store")
}

// LOGetWorksheet returns a worksheet (index starts at 0)
func LOGetWorksheet(document *ole.IDispatch, index int) (worksheet *ole.IDispatch) {
	sheets := oleutil.MustGetProperty(document, "Sheets").ToIDispatch()
	worksheet = oleutil.MustCallMethod(sheets, "getByIndex", index).ToIDispatch()
	return
}

// This example creates a new spreadsheet, reads and modifies cell values and style.
func main() {
	ole.CoInitialize(0)
	unknown, errCreate := oleutil.CreateObject("com.sun.star.ServiceManager")
	checkError(errCreate, "Couldn't create a OLE connection to LibreOffice")
	ServiceManager, errSM := unknown.QueryInterface(ole.IID_IDispatch)
	checkError(errSM, "Couldn't start a LibreOffice instance")
	desktop := oleutil.MustCallMethod(ServiceManager,
		"createInstance", "com.sun.star.frame.Desktop").ToIDispatch()

	document := LONewSpreadsheet(desktop)
	sheet0 := LOGetWorksheet(document, 0)

	cell1_1 := LOGetCell(sheet0, 1, 1) // cell B2
	cell1_2 := LOGetCell(sheet0, 1, 2) // cell B3
	cell1_3 := LOGetCell(sheet0, 1, 3) // cell B4
	cell1_4 := LOGetCell(sheet0, 1, 4) // cell B5
	LOSetCellString(cell1_1, "Hello World")
	LOSetCellValue(cell1_2, 33.45)
	LOSetCellFormula(cell1_3, "=B3+5")
	b4Value := LOGetCellString(cell1_3)
	LOSetCellString(cell1_4, b4Value)
	// set background color yellow:
	oleutil.MustPutProperty(cell1_1, "cellbackcolor", 0xFFFF00)

	fmt.Printf("Press [ENTER] to exit")
	fmt.Scanf("%s")
	ServiceManager.Release()
	ole.CoUninitialize()
}
