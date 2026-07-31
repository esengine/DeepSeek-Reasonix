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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"
)

// StringPtr returns a pointer to the string value passed in.
func StringPtr(v string) *string {
	return &v
}

// StringPtrIfNotEmpty returns a pointer to the string value passed in or
// nil if the value is empty.
func StringPtrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// StringValue returns the value of the string pointer passed in or
// "" if the pointer is nil.
func StringValue(v *string) string {
	if v != nil {
		return *v
	}
	return ""
}

// BoolPtr returns a pointer to the bool value passed in.
func BoolPtr(v bool) *bool {
	return &v
}

// BoolValue returns the value of the bool pointer passed in or
// false if the pointer is nil.
func BoolValue(v *bool) bool {
	if v != nil {
		return *v
	}
	return false
}

// IntPtr returns a pointer to the int value passed in.
func IntPtr(v int) *int {
	return &v
}

// IntPtrIfNotZero returns a pointer to the int value passed in or
// nil if the value is zero.
func IntPtrIfNotZero(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// IntValue returns the value of the int pointer passed in or
// 0 if the pointer is nil.
func IntValue(v *int) int {
	if v != nil {
		return *v
	}
	return 0
}

// Int8Ptr returns a pointer to the int8 value passed in.
func Int8Ptr(v int8) *int8 {
	return &v
}

// Int8Value returns the value of the int8 pointer passed in or
// 0 if the pointer is nil.
func Int8Value(v *int8) int8 {
	if v != nil {
		return *v
	}
	return 0
}

// Int16Ptr returns a pointer to the int16 value passed in.
func Int16Ptr(v int16) *int16 {
	return &v
}

// Int16Value returns the value of the int16 pointer passed in or
// 0 if the pointer is nil.
func Int16Value(v *int16) int16 {
	if v != nil {
		return *v
	}
	return 0
}

// Int32Ptr returns a pointer to the int32 value passed in.
func Int32Ptr(v int32) *int32 {
	return &v
}

// Int32Value returns the value of the int32 pointer passed in or
// 0 if the pointer is nil.
func Int32Value(v *int32) int32 {
	if v != nil {
		return *v
	}
	return 0
}

// Int64Ptr returns a pointer to the int64 value passed in.
func Int64Ptr(v int64) *int64 {
	return &v
}

// Int64Value returns the value of the int64 pointer passed in or
// 0 if the pointer is nil.
func Int64Value(v *int64) int64 {
	if v != nil {
		return *v
	}
	return 0
}

// Float32Ptr returns a pointer to the float32 value passed in.
func Float32Ptr(v float32) *float32 {
	return &v
}

// Float32Value returns the value of the float32 pointer passed in or
// 0 if the pointer is nil.
func Float32Value(v *float32) float32 {
	if v != nil {
		return *v
	}
	return 0
}

// Float64Ptr returns a pointer to the float64 value passed in.
func Float64Ptr(v float64) *float64 {
	return &v
}

// Float64Value returns the value of the float64 pointer passed in or
// 0 if the pointer is nil.
func Float64Value(v *float64) float64 {
	if v != nil {
		return *v
	}
	return 0
}

// TimePtr returns a pointer to the time.Time value passed in.
func TimePtr(v time.Time) *time.Time {
	return &v
}

// TimeValue returns the value of the time.Time pointer passed in or
// time.Time{} if the pointer is nil.
func TimeValue(v *time.Time) time.Time {
	if v != nil {
		return *v
	}
	return time.Time{}
}

// Prettify returns the string representation of a value.
func Prettify(i interface{}) string {
	var buf bytes.Buffer
	prettify(reflect.ValueOf(i), 0, &buf)
	return buf.String()
}

// DownloadFile returns the url of resource
func DownloadFile(ctx context.Context, url string) ([]byte, error) {
	r, err := downloadFileToStream(ctx, url)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return ioutil.ReadAll(r)
}

func extractAudFromURL(rawURL string) (string, error) {
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsedURL.Host != "" {
		return parsedURL.Host, nil
	}
	if parsedURL.Path != "" && !strings.Contains(parsedURL.Path, "/") {
		return parsedURL.Path, nil
	}
	return "", fmt.Errorf("invalid url : %s", rawURL)
}

func ResolveOAuthAud(config *Config) (string, error) {
	oauthBaseUrl, err := ResolveOAuthBaseUrl(config)
	if err != nil {
		return "", err
	}
	// The OAuth host, for example accounts.feishu.cn, is used as the client assertion audience.
	return extractAudFromURL(oauthBaseUrl)
}

func buildProxyURL(targetService, targetPrefix, apiPath string) string {
	if !strings.Contains(targetService, "://") {
		targetService = "https://" + targetService
	}
	return targetService + targetPrefix + apiPath
}

func ResolveOAuthBaseUrl(config *Config) (string, error) {
	if config.OAuthBaseUrl != "" {
		return normalizeBaseUrl(config.OAuthBaseUrl), nil
	}

	aud, err := extractAudFromURL(config.BaseUrl)
	if err != nil {
		return "", err
	}
	switch aud {
	case "open.feishu.cn":
		return "https://accounts.feishu.cn", nil
	case "open.larksuite.com":
		return "https://accounts.larksuite.com", nil
	default:
		return "", errors.New("OAuthBaseUrl is not configured. When BaseUrl is set to a non-default value (neither open.feishu.cn nor open.larksuite.com), you must explicitly configure OAuthBaseUrl via WithOAuthBaseUrl(...)")
	}
}

func normalizeBaseUrl(baseUrl string) string {
	if !strings.Contains(baseUrl, "://") {
		baseUrl = "https://" + baseUrl
	}
	return strings.TrimRight(baseUrl, "/")
}

type DecryptErr struct {
	Message string
}

func newDecryptErr(message string) *DecryptErr {
	return &DecryptErr{Message: message}
}

func (e DecryptErr) Error() string {
	return e.Message
}

func downloadFileToStream(ctx context.Context, url string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status code:%d", resp.StatusCode)
	}
	return resp.Body, nil
}

// prettify will recursively walk value v to build a textual
// representation of the value.
func prettify(v reflect.Value, indent int, buf *bytes.Buffer) {
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Struct:
		strType := v.Type().String()
		if strType == "time.Time" {
			fmt.Fprintf(buf, "%s", v.Interface())
			break
		} else if strings.HasPrefix(strType, "io.") {
			buf.WriteString("<buffer>")
			break
		}

		buf.WriteString("{\n")

		var names []string
		for i := 0; i < v.Type().NumField(); i++ {
			name := v.Type().Field(i).Name
			f := v.Field(i)
			if name[0:1] == strings.ToLower(name[0:1]) {
				continue // ignore unexported fields
			}
			if (f.Kind() == reflect.Ptr || f.Kind() == reflect.Slice || f.Kind() == reflect.Map) && f.IsNil() {
				continue // ignore unset fields
			}
			names = append(names, name)
		}

		for i, n := range names {
			val := v.FieldByName(n)
			buf.WriteString(strings.Repeat(" ", indent+2))
			buf.WriteString(n + ": ")
			prettify(val, indent+2, buf)

			if i < len(names)-1 {
				buf.WriteString(",\n")
			}
		}

		buf.WriteString("\n" + strings.Repeat(" ", indent) + "}")
	case reflect.Slice:
		strType := v.Type().String()
		if strType == "[]uint8" {
			fmt.Fprintf(buf, "<binary> len %d", v.Len())
			break
		}

		nl, id, id2 := "", "", ""
		if v.Len() > 3 {
			nl, id, id2 = "\n", strings.Repeat(" ", indent), strings.Repeat(" ", indent+2)
		}
		buf.WriteString("[" + nl)
		for i := 0; i < v.Len(); i++ {
			buf.WriteString(id2)
			prettify(v.Index(i), indent+2, buf)

			if i < v.Len()-1 {
				buf.WriteString("," + nl)
			}
		}

		buf.WriteString(nl + id + "]")
	case reflect.Map:
		buf.WriteString("{\n")

		for i, k := range v.MapKeys() {
			buf.WriteString(strings.Repeat(" ", indent+2))
			buf.WriteString(k.String() + ": ")
			prettify(v.MapIndex(k), indent+2, buf)

			if i < v.Len()-1 {
				buf.WriteString(",\n")
			}
		}

		buf.WriteString("\n" + strings.Repeat(" ", indent) + "}")
	default:
		if !v.IsValid() {
			fmt.Fprint(buf, "<invalid value>")
			return
		}
		format := "%v"
		switch v.Interface().(type) {
		case string:
			format = "%q"
		case io.ReadSeeker, io.Reader:
			format = "buffer(%p)"
		}
		fmt.Fprintf(buf, format, v.Interface())
	}
}

func StructToMap(val interface{}) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	s := reflect.Indirect(reflect.ValueOf(val))
	st := s.Type()
	for i := 0; i < s.NumField(); i++ {
		fieldDesc := st.Field(i)
		fieldVal := s.Field(i)
		if fieldDesc.Anonymous {
			embeddedMap, err := StructToMap(fieldVal.Interface())
			if err != nil {
				return nil, err
			}
			for k, v := range embeddedMap {
				m[k] = v
			}
			continue
		}
		jsonTag := fieldDesc.Tag.Get("json")
		if jsonTag == "" {
			continue
		}
		tag, err := parseJSONTag(jsonTag)
		if err != nil {
			return nil, err
		}
		if tag.ignore {
			continue
		}
		if fieldDesc.Type.Kind() == reflect.Ptr && fieldVal.IsNil() {
			continue
		}
		// nil maps are treated as empty maps.
		if fieldDesc.Type.Kind() == reflect.Map && fieldVal.IsNil() {
			continue
		}
		if fieldDesc.Type.Kind() == reflect.Slice && fieldVal.IsNil() {
			continue
		}
		if tag.stringFormat {
			m[tag.name] = formatAsString(fieldVal, fieldDesc.Type.Kind())
		} else {
			m[tag.name] = fieldVal.Interface()
		}
	}
	return m, nil
}

func formatAsString(v reflect.Value, kind reflect.Kind) string {
	if kind == reflect.Ptr && !v.IsNil() {
		v = v.Elem()
	}
	return fmt.Sprintf("%v", v.Interface())
}

type jsonTag struct {
	name         string
	stringFormat bool
	ignore       bool
}

func parseJSONTag(val string) (jsonTag, error) {
	if val == "-" {
		return jsonTag{ignore: true}, nil
	}
	var tag jsonTag
	i := strings.Index(val, ",")
	if i == -1 || val[:i] == "" {
		return tag, fmt.Errorf("malformed json tag: %s", val)
	}
	tag = jsonTag{
		name: val[:i],
	}
	switch val[i+1:] {
	case "omitempty":
	case "omitempty,string":
		tag.stringFormat = true
	default:
		return tag, fmt.Errorf("malformed json tag: %s", val)
	}
	return tag, nil
}

func isEmptyVal(v reflect.Value) bool {
	switch v.Kind() {
	// float
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	}
	return false
}

func userAgent(source string) string {
	ua := fmt.Sprintf("oapi-sdk-go/%s", SDKVersion())
	if source != "" {
		ua = ua + " source/" + source
	}
	return ua
}

// UserAgent returns the SDK User-Agent value.
func UserAgent(source string) string {
	return userAgent(source)
}

func readResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return respBody, nil
}

func File2Bytes(fileName string) ([]byte, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	fileData := make([]byte, fileInfo.Size())
	_, err = file.Read(fileData)
	if err != nil {
		return nil, err
	}
	return fileData, nil
}

func standardizeDataEn(data []byte) []byte {
	appendingLen := aes.BlockSize - (len(data) % aes.BlockSize)
	sd := make([]byte, len(data)+appendingLen)
	copy(sd, data)
	for i := 0; i < appendingLen; i++ {
		sd[i+len(data)] = byte(appendingLen)
	}
	return sd
}
func cBCEncrypter(buf []byte, keyStr string) ([]byte, error) {
	key := sha256.Sum256([]byte(keyStr))
	plaintext := standardizeDataEn(buf)

	if len(plaintext)%aes.BlockSize != 0 {
		return nil, errors.New("plaintext is not a multiple of the block size")
	}

	block, err := aes.NewCipher(key[:sha256.Size])
	if err != nil {
		return nil, err
	}

	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext[aes.BlockSize:], plaintext)

	if err != nil {
		return nil, err
	}

	return ciphertext, nil
}
func EncryptedEventMsg(ctx context.Context, data interface{}, encryptKey string) (string, error) {
	var bs []byte
	var err error

	switch data.(type) {
	case string:
		bs = []byte(data.(string))
	case []byte:
		bs = data.([]byte)
	default:
		bs, err = json.Marshal(data)
	}

	if err != nil {
		return "", err
	}

	encryptedData, err := cBCEncrypter(bs, encryptKey)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(encryptedData), nil
}
