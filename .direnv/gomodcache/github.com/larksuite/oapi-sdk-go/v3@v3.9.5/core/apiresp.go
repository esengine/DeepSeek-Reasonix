/*
 * MIT License
 *
 * Copyright (c) 2022 Lark Technologies Pte. Ltd.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice, shall be included in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package larkcore

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

type ApiResp struct {
	StatusCode int         `json:"-"`
	Header     http.Header `json:"-"`
	RawBody    []byte      `json:"-"`
}

func (resp ApiResp) Write(writer http.ResponseWriter) {
	writer.WriteHeader(resp.StatusCode)
	for k, vs := range resp.Header {
		for _, v := range vs {
			writer.Header().Add(k, v)
		}
	}
	if _, err := writer.Write(resp.RawBody); err != nil {
		panic(err)
	}
}

func (resp ApiResp) JSONUnmarshalBody(val interface{}, config *Config) error {
	if !strings.Contains(resp.Header.Get(contentTypeHeader), contentTypeJson) {
		return fmt.Errorf("response content-type not json, response: %v", resp)
	}
	return config.Serializable.Deserialize(resp.RawBody, val)
}

func (resp ApiResp) RequestId() string {
	logID := resp.Header.Get(HttpHeaderKeyLogId)
	if logID != "" {
		return logID
	}
	return resp.Header.Get(HttpHeaderKeyRequestId)
}

func (resp ApiResp) LogId() string {
	return resp.RequestId()
}

func (resp ApiResp) String() string {
	contentType := resp.Header.Get(contentTypeHeader)
	body := fmt.Sprintf("<binary> len %d", len(resp.RawBody))
	if strings.Contains(contentType, "json") || strings.Contains(contentType, "text") {
		body = string(resp.RawBody)
	}
	return fmt.Sprintf("StatusCode: %d, Header:%v, Content-Type: %s, Body: %v", resp.StatusCode,
		resp.Header, resp.Header.Get(contentTypeHeader), body)
}

type CodeError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Err  *struct {
		LogID                string                          `json:"log_id,omitempty"`
		Troubleshooter       string                          `json:"troubleshooter,omitempty"`
		Details              []*CodeErrorDetail              `json:"details,omitempty"`
		PermissionViolations []*CodeErrorPermissionViolation `json:"permission_violations,omitempty"`
		FieldViolations      []*CodeErrorFieldViolation      `json:"field_violations,omitempty"`
		Helps                []*CodeErrorHelp                `json:"helps,omitempty"`
	} `json:"error"`
}

func (ce CodeError) Error() string {
	return ce.String()
}

func (ce CodeError) ErrorResp() string {
	str, err := json.Marshal(ce)
	if err != nil {
		return ce.String()
	}
	return string(str)
}

func (ce CodeError) String() string {
	sb := strings.Builder{}
	sb.WriteString("msg:")
	sb.WriteString(ce.Msg)
	sb.WriteString(",code:")
	sb.WriteString(strconv.Itoa(ce.Code))
	return sb.String()
}

type CodeErrorDetail struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

type CodeErrorPermissionViolation struct {
	Type        string `json:"type,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Description string `json:"description,omitempty"`
}

type CodeErrorFieldViolation struct {
	Field       string `json:"field,omitempty"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

type CodeErrorHelp struct {
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
}

func FileNameByHeader(header http.Header) string {
	filename := ""
	_, media, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	if len(media) > 0 {
		filename = media["filename"]
	}
	return filename
}
