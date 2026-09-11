package systray

import (
	"fyne.io/systray/internal/generated/notifier"
	"github.com/godbus/dbus/v5"
)

type leftRightNotifierItem struct {
}

func newLeftRightNotifierItem() notifier.StatusNotifierItemer {
	return &leftRightNotifierItem{}
}

func (i *leftRightNotifierItem) Activate(_, _ int32) *dbus.Error {
	if f := tappedLeft; f == nil {
		return &dbus.ErrMsgUnknownMethod
	}

	tappedLeft()
	return nil
}

func (i *leftRightNotifierItem) ContextMenu(_, _ int32) *dbus.Error {
	if f := tappedRight; f == nil {
		return &dbus.ErrMsgUnknownMethod
	}

	tappedRight()
	return nil
}

func (i *leftRightNotifierItem) SecondaryActivate(_, _ int32) *dbus.Error {
	if f := tappedRight; f == nil {
		return &dbus.ErrMsgUnknownMethod
	}

	tappedRight()
	return nil
}

func (i *leftRightNotifierItem) Scroll(_ int32, _ string) *dbus.Error {
	return &dbus.ErrMsgUnknownMethod
}
