package registration

import "fmt"

type RegisterAppError struct {
	Code        string
	Description string
}

func (e *RegisterAppError) Error() string {
	return fmt.Sprintf("register app error: %s: %s", e.Code, e.Description)
}

type AccessDeniedError struct {
	*RegisterAppError
}

type ExpiredError struct {
	*RegisterAppError
}
