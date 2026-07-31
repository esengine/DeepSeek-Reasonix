// +build windows

package main

import (
	"fmt"
	"log"
	"os"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

func writeExample(excel, workbooks *ole.IDispatch, filepath string) {
	// ref: https://msdn.microsoft.com/zh-tw/library/office/ff198017.aspx
	// http://stackoverflow.com/questions/12159513/what-is-the-correct-xlfileformat-enumeration-for-excel-97-2003
	const xlExcel8 = 56
	workbook := oleutil.MustCallMethod(workbooks, "Add", nil).ToIDispatch()
	defer workbook.Release()
	worksheet := oleutil.MustGetProperty(workbook, "Worksheets", 1).ToIDispatch()
	defer worksheet.Release()
	cell := oleutil.MustGetProperty(worksheet, "Cells", 1, 1).ToIDispatch()
	oleutil.PutProperty(cell, "Value", 12345)
	cell.Release()
	activeWorkBook := oleutil.MustGetProperty(excel, "ActiveWorkBook").ToIDispatch()
	defer activeWorkBook.Release()

	os.Remove(filepath)
	// ref: https://msdn.microsoft.com/zh-tw/library/microsoft.office.tools.excel.workbook.saveas.aspx
	oleutil.MustCallMethod(activeWorkBook, "SaveAs", filepath, xlExcel8, nil, nil).ToIDispatch()

	//time.Sleep(2 * time.Second)

	// let excel could close without asking
	// oleutil.PutProperty(workbook, "Saved", true)
	// oleutil.CallMethod(workbook, "Close", false)
}

func readExample(fileName string, excel, workbooks *ole.IDispatch) {
	workbook, err := oleutil.CallMethod(workbooks, "Open", fileName)

	if err != nil {
		log.Fatalln(err)
	}
	defer workbook.ToIDispatch().Release()

	sheets := oleutil.MustGetProperty(excel, "Sheets").ToIDispatch()
	sheetCount := (int)(oleutil.MustGetProperty(sheets, "Count").Val)
	fmt.Println("sheet count=", sheetCount)
	sheets.Release()

	worksheet := oleutil.MustGetProperty(workbook.ToIDispatch(), "Worksheets", 1).ToIDispatch()
	defer worksheet.Release()
	for row := 1; row <= 2; row++ {
		for col := 1; col <= 5; col++ {
			cell := oleutil.MustGetProperty(worksheet, "Cells", row, col).ToIDispatch()
			val, err := oleutil.GetProperty(cell, "Value")
			if err != nil {
				break
			}
			fmt.Printf("(%d,%d)=%+v toString=%s\n", col, row, val.Value(), val.ToString())
			cell.Release()
		}
	}
}

func showMethodsAndProperties(i *ole.IDispatch) {
	n, err := i.GetTypeInfoCount()
	if err != nil {
		log.Fatalln(err)
	}
	tinfo, err := i.GetTypeInfo()
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println("n=", n, "tinfo=", tinfo)
}

func main() {
	log.SetFlags(log.Flags() | log.Lshortfile)
	ole.CoInitialize(0)
	unknown, _ := oleutil.CreateObject("Excel.Application")
	excel, _ := unknown.QueryInterface(ole.IID_IDispatch)
	oleutil.PutProperty(excel, "Visible", true)

	workbooks := oleutil.MustGetProperty(excel, "Workbooks").ToIDispatch()
	cwd, _ := os.Getwd()
	writeExample(excel, workbooks, cwd+"\\write.xls")
	readExample(cwd+"\\excel97-2003.xls", excel, workbooks)
	showMethodsAndProperties(workbooks)
	workbooks.Release()
	// oleutil.CallMethod(excel, "Quit")
	excel.Release()
	ole.CoUninitialize()
}
