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

package larkbitable

func (a *AppTableRecord) StringField(key string) *string {
	if a == nil || a.Fields == nil {
		return nil
	}

	v := a.Fields[key]
	if v == nil {
		return nil
	}

	if v, ok := v.(string); ok {
		return &v
	}

	return nil
}

func (a *AppTableRecord) ListStringField(key string) []string {
	if a == nil || a.Fields == nil {
		return nil
	}
	if v, ok := a.Fields[key].([]string); ok {
		return v
	}

	return nil
}

func (a *AppTableRecord) BoolField(key string) *bool {
	if a == nil || a.Fields == nil {
		return nil
	}
	if v, ok := a.Fields[key].(bool); ok {
		return &v
	}

	return nil
}

func (a *AppTableRecord) ListUrlField(key string) []Url {
	if v, ok := a.Fields[key].([]Url); ok {
		return v
	}

	return nil
}

func (a *AppTableRecord) ListPersonField(key string) []Person {
	if v, ok := a.Fields[key].([]Person); ok {
		return v
	}

	return nil
}

func (a *AppTableRecord) ListAttachmentField(key string) []Attachment {
	if v, ok := a.Fields[key].([]Attachment); ok {
		return v
	}
	return nil
}
