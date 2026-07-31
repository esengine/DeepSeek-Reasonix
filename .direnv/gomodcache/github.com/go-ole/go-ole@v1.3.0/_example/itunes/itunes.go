// +build windows

package main

import (
	"log"
	"os"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/gonuts/commander"
)

func iTunes() *ole.IDispatch {
	ole.CoInitialize(0)
	unknown, err := oleutil.CreateObject("iTunes.Application")
	if err != nil {
		log.Fatal(err)
	}
	itunes, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		log.Fatal(err)
	}
	return itunes
}

func main() {
	command := &commander.Command{
		UsageLine: os.Args[0],
		Short:     "itunes cmd",
	}
	command.Subcommands = []*commander.Command{}
	for _, name := range []string{"Play", "Stop", "Pause", "Quit"} {
		command.Subcommands = append(command.Subcommands, &commander.Command{
			Run: func(cmd *commander.Command, args []string) error {
				_, err := oleutil.CallMethod(iTunes(), name)
				return err
			},
			UsageLine: strings.ToLower(name),
		})
	}
	err := command.Dispatch(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
}
