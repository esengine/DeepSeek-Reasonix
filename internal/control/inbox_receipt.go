package control

import (
	"errors"
	"reasonix/internal/sessioninbox"
)

var ErrInboxSessionChanged = errors.New("inbox session changed before submission")

func (c *Controller) LookupInboxReceipt(key string) (sessioninbox.InboxReceipt, bool, error) {
	return c.LookupInboxReceiptForSession("", key)
}

func (c *Controller) LookupInboxReceiptForSession(path, key string) (sessioninbox.InboxReceipt, bool, error) {
	store, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxReceipt{}, false, err
	}
	if path != "" && store.SessionPath() != path {
		return sessioninbox.InboxReceipt{}, false, ErrInboxSessionChanged
	}
	receipt, found := store.LookupReceipt(key)
	return receipt, found, nil
}
