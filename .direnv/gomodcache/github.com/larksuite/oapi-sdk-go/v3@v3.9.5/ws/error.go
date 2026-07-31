package ws

import "fmt"

type ClientError struct {
	Code int
	Msg  string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("%d: %s", e.Code, e.Msg)
}

func NewClientError(code int, msg string) *ClientError {
	return &ClientError{
		code,
		msg,
	}
}

type ServerError struct {
	Code int
	Msg  string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("%d: %s", e.Code, e.Msg)
}

func NewServerError(code int, msg string) *ServerError {
	return &ServerError{
		code,
		msg,
	}
}
