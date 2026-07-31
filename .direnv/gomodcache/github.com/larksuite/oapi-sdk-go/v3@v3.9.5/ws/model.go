package ws

import "strconv"

type EndpointResp struct {
	Code int       `json:"code"`
	Msg  string    `json:"msg"`
	Data *Endpoint `json:"data"`
}

type Endpoint struct {
	Url          string        `json:"URL,omitempty"`
	ClientConfig *ClientConfig `json:"ClientConfig,omitempty"`
}

type BootstrapRequest struct {
	AppID           string `json:"AppID"`
	AppSecret       string `json:"AppSecret"`
	ClientAssertion string `json:"ClientAssertion"`
}

// ClientConfig 由服务端下发
type ClientConfig struct {
	ReconnectCount    int `json:"ReconnectCount,omitempty"`
	ReconnectInterval int `json:"ReconnectInterval,omitempty"`
	ReconnectNonce    int `json:"ReconnectNonce,omitempty"`
	PingInterval      int `json:"PingInterval,omitempty"`
}

// Headers Frame.Headers
type Headers []Header

func (h Headers) GetString(key string) string {
	for _, header := range h {
		if header.Key == key {
			return header.Value
		}
	}

	return ""
}

func (h Headers) GetInt(key string) int {
	for _, header := range h {
		if header.Key == key {
			if val, err := strconv.Atoi(header.Value); err == nil {
				return val
			}
		}
	}

	return 0
}

func (h *Headers) Add(key, value string) {
	header := Header{
		Key:   key,
		Value: value,
	}
	*h = append(*h, header)
}

// Response 上行响应消息结构, 置于 Frame.Payload
type Response struct {
	StatusCode int               `json:"code"`
	Headers    map[string]string `json:"headers"`
	Data       []byte            `json:"data"`
}

func NewResponseByCode(code int) *Response {
	return &Response{
		StatusCode: code,
	}
}

func NewPingFrame(serviceID int32) *Frame {
	headers := Headers{}
	headers.Add(HeaderType, string(MessageTypePing))
	return &Frame{
		Method:  int32(FrameTypeControl),
		Service: serviceID,
		Headers: headers,
	}
}
