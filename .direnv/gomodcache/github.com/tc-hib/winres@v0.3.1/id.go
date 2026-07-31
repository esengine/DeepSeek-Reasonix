package winres

import (
	"errors"
	"strings"
)

// ID is the type of a resource id, or resource type id.
type ID uint16

// Name is the type of a resource name, or a resource type name.
type Name string

// Identifier is either an ID or a Name.
//
// When you are asked for an Identifier, you can pass an int cast to an ID or a string cast to a Name.
type Identifier interface {
	// This method serves both to seal the interface and help order identifiers the standard way
	// https://docs.microsoft.com/en-us/windows/win32/debug/pe-format#resource-directory-entries
	lessThan(ident Identifier) bool
}

func (id ID) lessThan(ident Identifier) bool {
	right, ok := ident.(ID)
	return ok && id < right
}

func (n Name) lessThan(ident Identifier) bool {
	right, ok := ident.(Name)
	return !ok || n < right
}

func checkIdentifier(ident Identifier) error {
	switch ident := ident.(type) {
	case ID:
		if ident == 0 {
			return errors.New(errZeroID)
		}
	case Name:
		if ident == "" {
			return errors.New(errEmptyName)
		}
		if strings.ContainsRune(string(ident), 0) {
			return errors.New(errNameContainsNUL)
		}
	}
	return nil
}
