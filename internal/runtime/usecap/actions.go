package usecap

import "slices"

// The actions use_capability declares in its schema.
const (
	ActionSearch  = "search"
	ActionList    = "list"
	ActionInspect = "inspect"
	ActionCall    = "call"
	ActionDecline = "decline"
)

// CatalogReadActions resolve host-side without a target and execute nothing.
// inspect of a connected server may refresh the in-memory tool snapshot.
var CatalogReadActions = []string{ActionSearch, ActionList, ActionInspect}

// IsCatalogReadAction reports whether action is one of CatalogReadActions.
func IsCatalogReadAction(action string) bool {
	return slices.Contains(CatalogReadActions, action)
}

// IDlessCatalogActions are the catalog reads that need no capability_id. inspect
// is excluded on purpose: it names an id, so a restricted proxy must gate it.
var IDlessCatalogActions = []string{ActionSearch, ActionList}

// IsIDlessCatalogAction reports whether action is one of IDlessCatalogActions.
func IsIDlessCatalogAction(action string) bool {
	return slices.Contains(IDlessCatalogActions, action)
}
