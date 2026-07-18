package runtimeapi

import "fmt"

func ValidateHistoryTurns(turns int) error {
	if turns < HistoryMinTurns || turns > HistoryMaxTurns {
		return fmt.Errorf("runtimeapi: history turns must be in %d..%d", HistoryMinTurns, HistoryMaxTurns)
	}
	return nil
}

// ValidatePageLimit accepts zero as the shared default-page sentinel.
func ValidatePageLimit(limit int) error {
	if limit != 0 && (limit < 1 || limit > PageMaxItems) {
		return fmt.Errorf("runtimeapi: page limit must be zero or in 1..%d", PageMaxItems)
	}
	return nil
}

// ValidateSearchLimit accepts zero as the shared default-search sentinel.
func ValidateSearchLimit(limit int) error {
	if limit != 0 && (limit < 1 || limit > SearchMaxItems) {
		return fmt.Errorf("runtimeapi: search limit must be zero or in 1..%d", SearchMaxItems)
	}
	return nil
}
