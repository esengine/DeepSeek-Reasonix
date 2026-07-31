package toast

import "errors"

var (
	ErrorInvalidAudio    error = errors.New("toast: invalid audio")
	ErrorInvalidDuration       = errors.New("toast: invalid duration")
)

// toastAudio identifies audio that Windows can play.
type toastAudio = string

const (
	Default        toastAudio = "ms-winsoundevent:Notification.Default"
	IM             toastAudio = "ms-winsoundevent:Notification.IM"
	Mail           toastAudio = "ms-winsoundevent:Notification.Mail"
	Reminder       toastAudio = "ms-winsoundevent:Notification.Reminder"
	SMS            toastAudio = "ms-winsoundevent:Notification.SMS"
	LoopingAlarm   toastAudio = "ms-winsoundevent:Notification.Looping.Alarm"
	LoopingAlarm2  toastAudio = "ms-winsoundevent:Notification.Looping.Alarm2"
	LoopingAlarm3  toastAudio = "ms-winsoundevent:Notification.Looping.Alarm3"
	LoopingAlarm4  toastAudio = "ms-winsoundevent:Notification.Looping.Alarm4"
	LoopingAlarm5  toastAudio = "ms-winsoundevent:Notification.Looping.Alarm5"
	LoopingAlarm6  toastAudio = "ms-winsoundevent:Notification.Looping.Alarm6"
	LoopingAlarm7  toastAudio = "ms-winsoundevent:Notification.Looping.Alarm7"
	LoopingAlarm8  toastAudio = "ms-winsoundevent:Notification.Looping.Alarm8"
	LoopingAlarm9  toastAudio = "ms-winsoundevent:Notification.Looping.Alarm9"
	LoopingAlarm10 toastAudio = "ms-winsoundevent:Notification.Looping.Alarm10"
	LoopingCall    toastAudio = "ms-winsoundevent:Notification.Looping.Call"
	LoopingCall2   toastAudio = "ms-winsoundevent:Notification.Looping.Call2"
	LoopingCall3   toastAudio = "ms-winsoundevent:Notification.Looping.Call3"
	LoopingCall4   toastAudio = "ms-winsoundevent:Notification.Looping.Call4"
	LoopingCall5   toastAudio = "ms-winsoundevent:Notification.Looping.Call5"
	LoopingCall6   toastAudio = "ms-winsoundevent:Notification.Looping.Call6"
	LoopingCall7   toastAudio = "ms-winsoundevent:Notification.Looping.Call7"
	LoopingCall8   toastAudio = "ms-winsoundevent:Notification.Looping.Call8"
	LoopingCall9   toastAudio = "ms-winsoundevent:Notification.Looping.Call9"
	LoopingCall10  toastAudio = "ms-winsoundevent:Notification.Looping.Call10"
	Silent         toastAudio = "silent"
)

// toastduration identifies toast duration for audio playback.
type toastDuration = string

const (
	Short toastDuration = "short"
	Long  toastDuration = "long"
)

// ActivationType identifies the method that Windows Runtime will use to handle
// notification interactions.
//
// See https://learn.microsoft.com/en-us/dotnet/api/microsoft.toolkit.uwp.notifications.toastactivationtype
type ActivationType = string

const (
	// Protocol is for launching third-party applications using a protocol uri, like https or mailto.
	Protocol ActivationType = "protocol"
	// Foreground is for launching your foreground application. This is required to enable the activation
	// callback. There is a third option: Background, however for Desktop applications Foreground and
	// Background behave identically.
	//
	// See https://learn.microsoft.com/en-us/windows/apps/design/shell/tiles-and-notifications/send-local-toast-desktop-cpp-wrl#foreground-vs-background-activation
	Foreground ActivationType = "foreground"
)
