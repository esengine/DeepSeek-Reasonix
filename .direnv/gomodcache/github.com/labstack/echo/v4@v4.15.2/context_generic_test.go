// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextGetOK(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)

	c.Set("key", int64(123))

	v, err := ContextGet[int64](c, "key")
	assert.NoError(t, err)
	assert.Equal(t, int64(123), v)
}

func TestContextGetNonExistentKey(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)

	c.Set("key", int64(123))

	v, err := ContextGet[int64](c, "nope")
	assert.ErrorIs(t, err, ErrNonExistentKey)
	assert.Equal(t, int64(0), v)
}

func TestContextGetInvalidCast(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)

	c.Set("key", int64(123))

	v, err := ContextGet[bool](c, "key")
	assert.ErrorIs(t, err, ErrInvalidKeyType)
	assert.Equal(t, false, v)
}

func TestContextGetOrOK(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)

	c.Set("key", int64(123))

	v, err := ContextGetOr[int64](c, "key", 999)
	assert.NoError(t, err)
	assert.Equal(t, int64(123), v)
}

func TestContextGetOrNonExistentKey(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)

	c.Set("key", int64(123))

	v, err := ContextGetOr[int64](c, "nope", 999)
	assert.NoError(t, err)
	assert.Equal(t, int64(999), v)
}

func TestContextGetOrInvalidCast(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)

	c.Set("key", int64(123))

	v, err := ContextGetOr[float32](c, "key", float32(999))
	assert.ErrorIs(t, err, ErrInvalidKeyType)
	assert.Equal(t, float32(0), v)
}
