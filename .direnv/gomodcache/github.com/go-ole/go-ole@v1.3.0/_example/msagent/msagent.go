// +build windows

package main

import (
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

func main() {
	ole.CoInitialize(0)
	unknown, _ := oleutil.CreateObject("Agent.Control.1")
	agent, _ := unknown.QueryInterface(ole.IID_IDispatch)
	oleutil.PutProperty(agent, "Connected", true)
	characters := oleutil.MustGetProperty(agent, "Characters").ToIDispatch()
	oleutil.CallMethod(characters, "Load", "Merlin", "c:\\windows\\msagent\\chars\\Merlin.acs")
	character := oleutil.MustCallMethod(characters, "Character", "Merlin").ToIDispatch()
	oleutil.CallMethod(character, "Show")
	oleutil.CallMethod(character, "Speak", "こんにちわ世界")

	time.Sleep(4000000000)
}
