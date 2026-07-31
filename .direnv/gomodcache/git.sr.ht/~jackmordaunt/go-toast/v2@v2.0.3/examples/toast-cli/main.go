package main

import (
	"flag"
	"fmt"
	"time"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
)

func main() {
	var (
		n           toast.Notification
		wait        bool
		showActions bool
	)

	flag.StringVar(&n.AppID, "app-id", "com.windows.toast.cli", "Application ID that identifies this app")
	flag.StringVar(&n.Title, "title", "title", "Title")
	flag.StringVar(&n.Body, "body", "body", "Body")
	flag.StringVar(&n.Icon, "icon", "", "Icon")
	flag.StringVar(&n.ActivationType, "activation-type", toast.Foreground, "Activation Type [protocol, foreground, background]")
	flag.StringVar(&n.ActivationArguments, "activation-args", "", "Activation Arguments")
	flag.StringVar(&n.Audio, "audio", "", "Audio to play when displaying the toast")
	flag.StringVar(&n.ActivationExe, "activation-exe", "", "path to activation executable")
	flag.BoolVar(&n.Loop, "loop", false, "Loop audio")
	flag.StringVar(&n.Duration, "duration", "short", "Audio duration")

	flag.BoolVar(&wait, "wait", false, "Wait for activation")
	flag.BoolVar(&showActions, "demo-actions", false, "Display preconfigured actions for demonstration")

	flag.Parse()

	n.Inputs = append(n.Inputs, toast.Input{
		ID:          "reply-to:john-doe",
		Title:       "Reply",
		Placeholder: "Reply to John Doe",
	})

	n.Inputs = append(n.Inputs, toast.Input{
		ID:          "select-action",
		Title:       "Selection Action",
		Placeholder: "Pick an action to perform",
		Selections: []toast.InputSelection{
			{
				ID:      "1",
				Content: "do thing one",
			},
			{
				ID:      "2",
				Content: "do thing two",
			},
			{
				ID:      "3",
				Content: "do thing three",
			},
		},
	})

	n.Actions = append(n.Actions, toast.Action{
		Type:      toast.Foreground,
		Content:   "Send",
		Arguments: "send",
	})
	n.Actions = append(n.Actions, toast.Action{
		Type:      toast.Foreground,
		Content:   "Close",
		Arguments: "close",
	})

	if wait {
		// Dummy goroutine to stop the runtime from thinking we are deadlocked when
		// waiting on C code to call us back.
		go func() {
			for range time.NewTicker(time.Second).C {
			}
		}()

		done := make(chan struct{})
		defer func() {
			<-done
		}()

		toast.SetActivationCallback(func(args string, data []toast.UserData) {
			fmt.Printf("OnActivate args: %q, userdata: %v\n", args, data)
			done <- struct{}{}
		})

	} else {
		toast.SetActivationCallback(func(_ string, _ []toast.UserData) {
			fmt.Printf("OnActivate\n")
		})
	}

	if err := n.Push(); err != nil {
		fmt.Printf("error: %v\n", err)
	}
}
