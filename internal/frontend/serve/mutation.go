package serve

import (
	"errors"
	"net/http"

	"reasonix/internal/state/mutation"
)

func refuseMutation(w http.ResponseWriter, err error) bool {
	var version *mutation.VersionConflict
	var wait *mutation.WaitError
	switch {
	case errors.As(err, &version):
		refuse(w, http.StatusConflict, version.RefusalCode(), err.Error(), map[string]any{"path": version.Path})
	case errors.As(err, &wait):
		refuse(w, http.StatusConflict, wait.RefusalCode(), err.Error(), map[string]any{"transaction_id": wait.TransactionID, "paths": wait.Paths})
	default:
		return false
	}
	return true
}
