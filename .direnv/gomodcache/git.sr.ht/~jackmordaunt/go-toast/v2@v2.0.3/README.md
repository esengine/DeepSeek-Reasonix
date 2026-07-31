# go-toast

This package implements Windows toast notifications using the Windows Runtime COM API. 

The XML schema used to describe such notifications is here: 

https://learn.microsoft.com/en-us/windows/apps/design/shell/tiles-and-notifications/adaptive-interactive-toasts

Package `wintoast` offers a lower-level api.
Package `toast` offers a higher-level wrapper. 

`wintoast` uses build tags to guard Windows only code. It will still compile on 
non-Windows platforms, however the functions are stubbed out and will do nothing 
when invoked. 

## Usage

### Basic

```go
noti := toast.Notification{
    AppID: "My cool app",
    Title: "Title",
    Body: "Body",
}

err := noti.Push()
```

### Actions / Inputs with Callback

Additionally, we can respond to notification activation with a callback. 

```go
// Set the callback that receives the data from the notification.
// Any data from actions or inputs will be accessible here. 
toast.SetActivationCallback(func(args string, data []UserData) {
    fmt.Printf("args: %q, data: %v\n", args, data)
})

n := toast.Notification{
    AppID: "My cool app",
    Title: "Title",
    Body: "Body", 
}

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

err := n.Push()
```