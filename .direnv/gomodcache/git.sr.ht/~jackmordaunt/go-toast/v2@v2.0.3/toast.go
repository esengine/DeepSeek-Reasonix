// Package toast wraps the lower-level wintoast api and provides an easy way
// to send and respond to toast notifications on Windows.
//
// First, setup your AppData vis SetAppData function. This will install your
// application metadata into the Windows Registry.
//
// Then, if you want in-process callback to be invoked upon user interaction,
// invoke SetActivationCallback.
//
// Finally, generate your notification by instantiation a toast.Notification
// and pushing it with Push method.
package toast

import (
	"bytes"

	"git.sr.ht/~jackmordaunt/go-toast/v2/tmpl"
	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
)

// Notification
//
// The toast notification data. The following fields are strongly recommended;
//   - AppID
//   - Title
//
// If no toastAudio is provided, then the toast notification will be silent.
//
// The AppID is shown beneath the toast message (in certain cases), and above the notification within the Action
// Center - and is used to group your notifications together. It is recommended that you provide a "pretty"
// name for your app, and not something like "com.example.MyApp". It can be ellided if the value has already
// been set via SetAppData.
//
// If no Title is provided, but a Body is, the body will display as the toast notification's title -
// which is a slightly different font style (heavier).
//
// The Icon should be an absolute path to the icon (as the toast is invoked from a temporary path on the user's
// system, not the working directory).
//
// If you would like the toast to call an external process/open a webpage, then you can set ActivationArguments
// to the uri you would like to trigger when the toast is clicked. For example: "https://google.com" would open
// the Google homepage when the user clicks the toast notification.
// By default, clicking the toast just hides/dismisses it.
//
// The following would show a notification to the user letting them know they received an email, and opens
// gmail.com when they click the notification. It also makes the Windows 10 "mail" sound effect.
//
//	toast := toast.Notification{
//	    AppID:               "Google Mail",
//	    Title:               email.Subject,
//	    Message:             email.Preview,
//	    Icon:                "C:/Program Files/Google Mail/icons/logo.png",
//	    ActivationArguments: "https://gmail.com",
//	    Audio:               toast.Mail,
//	}
//
//	err := toast.Push()
type Notification struct {
	// The name of your app. This value shows up in Windows Action Centre, so make it
	// something readable for your users.
	AppID string

	// The main title/heading for the toast notification.
	Title string

	// The single/multi line message to display for the toast notification.
	Body string

	// An optional path to an image on the OS to display to the left of the title & message.
	Icon string

	// An optional crop style for the Icon.
	IconCrop CropStyle

	// An optional path to an image to display as a bold hero image.
	HeroIcon string

	// A color to show as the icon background.
	IconBackgroundColor string

	// Action to take when the notification is as a whole activated.
	ActivationType ActivationType

	// The activation/action arguments (invoked when the user clicks the notification).
	// This is returned to the callback when activated.
	ActivationArguments string

	// Optional text input to display before the actions.
	Inputs []Input

	// Optional action buttons to display below the notification title & message.
	Actions []Action

	// The audio to play when displaying the toast
	Audio toastAudio

	// Whether to loop the audio (default false).
	Loop bool

	// How long the toast should show up for (short/long).
	Duration toastDuration

	// This is an absolute path to an executable that will launched by the
	// Windows Runtime when the COM server is not running. This executable must be able
	// to handle the -Embedding flag that Windows invokes it with.
	ActivationExe string
}

// CropStyle specifies the hint-crop attribute for an image.
type CropStyle = string

const (
	CropStyleEmpty  CropStyle = ""
	CropStyleSquare CropStyle = "square"
	CropStyleCircle CropStyle = "circle"
)

// UserData contains user supplied data from the notification, such as text input
// or a selection.
type UserData = wintoast.UserData

// Input
//
// Defines an input element, generally a text input.
// See  https://learn.microsoft.com/en-us/uwp/schemas/tiles/toastschema/element-input for more info.
//
// Inputs are by default textual, however if selections are supplied the input will be rendered
// as a select input.
type Input struct {
	ID          string
	Title       string
	Placeholder string
	Selections  []InputSelection
}

// InputSelection
//
// Defines an input selection for use with select inputs.
// See https://learn.microsoft.com/en-us/uwp/schemas/tiles/toastschema/element-selection for more info.
type InputSelection struct {
	ID      string
	Content string
}

// Action
//
// Defines an actionable button.
// See https://msdn.microsoft.com/en-us/windows/uwp/controls-and-patterns/tiles-and-notifications-adaptive-interactive-toasts for more info.
//
//	toast.Action{toast.Protocol, "Open Maps", "bingmaps:?q=sushi"}
//
// TODO(jfm): we can likely support an activation callback directly in the Action.
type Action struct {
	Type      ActivationType
	Content   string
	Arguments string
	InputID   string // optional ID of any related input, affects styling.
}

// Push the notification to the Windows Runtime via the COM API.
// Ensure [SetAppData] has been called prior to pushing notifications.
//
//	notification := toast.Notification{
//	    AppID: "Example App",
//	    Title: "My notification",
//	    Message: "Some message about how important something is...",
//	    Icon: "go.png",
//	    Actions: []toast.Action{
//	        {"protocol", "I'm a button", ""},
//	        {"protocol", "Me too!", ""},
//	    },
//	}
//	err := notification.Push()
//	if err != nil {
//	    log.Fatalln(err)
//	}
func (n *Notification) Push() error {
	n.applyDefaults()
	xml, err := n.buildXML()
	if err != nil {
		return err
	}
	return wintoast.Push(n.AppID, xml, wintoast.PowershellFallback)
}

func (n *Notification) applyDefaults() {
	if n.ActivationType == "" {
		n.ActivationType = Foreground
	}
	if n.Duration == "" {
		n.Duration = Short
	}
	if n.Audio == "" {
		n.Audio = Default
	}
}

func (n *Notification) buildXML() (string, error) {
	var out bytes.Buffer
	err := tmpl.XMLTemplate.Execute(&out, n)
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// SetActivationCallback sets the global activation callback.
//
// The first argument contains application defined data (embedded within the xml),
// which is how the callback knows which part of the toast was activated.
// Argument data is defined by `toast.Action.Arguments` on the notification.
//
// The second argument contains user defined data (input/selected by user).
// All elements of user input will be supplied here, even if the value is empty.
// User inputs correspond to all `toast.Input`s defined on the notification.
//
// This function will be invoked when a toast notification is interacted with.
//
// This will do nothing if the the powershell fallback is in-effect.
func SetActivationCallback(cb func(args string, data []UserData)) {
	wintoast.SetActivationCallback(func(appUserModelId, invokedArgs string, userData []wintoast.UserData) {
		cb(invokedArgs, userData)
	})
}

type AppData = wintoast.AppData

// SetAppData sets application metadata in the Windows Registry.
// This is required to display the application name, as well as any branding.
// Registry is global state, hence it makes sense to set it global.
func SetAppData(data AppData) error {
	return wintoast.SetAppData(data)
}
