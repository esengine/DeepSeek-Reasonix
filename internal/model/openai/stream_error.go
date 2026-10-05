package openai

type streamWireError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Type    string `json:"type"`
	Param   string `json:"param"`
}
