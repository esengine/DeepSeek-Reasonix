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
	"bytes"
	"context"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTranslate(t *testing.T) {
	config := mockConfig()
	reqTranslator := ReqTranslator{}
	_, err := reqTranslator.translate(context.Background(), &ApiReq{
		HttpMethod: http.MethodPost,
		ApiPath:    "https://www.feishu.cn/approval/openapi/v2/approval/get",
		Body: map[string]interface{}{
			"approval_code": "ou_c245b0a7dff2725cfa2fb104f8b48b9d",
		}}, AccessTokenTypeTenant, config, &RequestOption{
		TenantAccessToken: "ssss",
	})

	if err != nil {
		t.Errorf("TestTranslate failed ,%v", err)
	}

}

func TestPathUrlEncode(t *testing.T) {
	url, _ := reqTranslator.getFullReqUrl("https://open.feishu.com", "open-apis/:a/:b/:c/:d", map[string]interface{}{"a": 12, "b": "sssss", "c": "12121wwww", "d": "加多"}, map[string]interface{}{"user_type": "open_id"})
	fmt.Println(url)

	if url != "https://open.feishu.comopen-apis/12/sssss/12121wwww/%E5%8A%A0%E5%A4%9A?user_type=open_id" {
		t.Errorf("TestPathUrlEncode failed ")
	}
}

func TestFormdataContentUsesReaderNameAsFilename(t *testing.T) {
	formData := NewFormdata().
		AddField("file_type", "stream").
		AddFile("file", &testNamedReader{
			Reader: bytes.NewReader([]byte("hello")),
			name:   "/tmp/report.txt",
		})

	form, err := readMultipartForm(formData)
	if err != nil {
		t.Fatalf("read multipart form failed: %v", err)
	}

	if got := form.Value["file_type"][0]; got != "stream" {
		t.Fatalf("field file_type = %q, want %q", got, "stream")
	}
	if got := form.File["file"][0].Filename; got != "report.txt" {
		t.Fatalf("filename = %q, want %q", got, "report.txt")
	}
}

func TestFormdataAddFileWithName(t *testing.T) {
	formData := NewFormdata().
		AddFileWithName("file", "/tmp/from-nop-closer.txt", testReadCloser{Reader: bytes.NewReader([]byte("hello"))})

	form, err := readMultipartForm(formData)
	if err != nil {
		t.Fatalf("read multipart form failed: %v", err)
	}

	if got := form.File["file"][0].Filename; got != "from-nop-closer.txt" {
		t.Fatalf("filename = %q, want %q", got, "from-nop-closer.txt")
	}
}

func TestFormdataAddFileByPath(t *testing.T) {
	tempFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("formdata-file-%d.txt", time.Now().UnixNano()))
	file, err := os.OpenFile(tempFilePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatalf("create temp file failed: %v", err)
	}
	defer os.Remove(file.Name())

	_, err = file.WriteString("hello from path")
	if err != nil {
		t.Fatalf("write temp file failed: %v", err)
	}
	err = file.Close()
	if err != nil {
		t.Fatalf("close temp file failed: %v", err)
	}

	formData := NewFormdata().AddFileByPath("file", file.Name())
	form, err := readMultipartForm(formData)
	if err != nil {
		t.Fatalf("read multipart form failed: %v", err)
	}

	fileHeader := form.File["file"][0]
	if got, want := fileHeader.Filename, filepath.Base(file.Name()); got != want {
		t.Fatalf("filename = %q, want %q", got, want)
	}

	uploaded, err := fileHeader.Open()
	if err != nil {
		t.Fatalf("open multipart file failed: %v", err)
	}
	defer uploaded.Close()

	var content bytes.Buffer
	_, err = content.ReadFrom(uploaded)
	if err != nil {
		t.Fatalf("read multipart file failed: %v", err)
	}
	if got := content.String(); got != "hello from path" {
		t.Fatalf("content = %q, want %q", got, "hello from path")
	}
}

type testNamedReader struct {
	*bytes.Reader
	name string
}

func (r *testNamedReader) Name() string {
	return r.name
}

type testReadCloser struct {
	*bytes.Reader
}

func (r testReadCloser) Close() error {
	return nil
}

func readMultipartForm(formData *Formdata) (*multipart.Form, error) {
	contentType, body, err := formData.content()
	if err != nil {
		return nil, err
	}

	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}

	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	return reader.ReadForm(int64(len(body)))
}
