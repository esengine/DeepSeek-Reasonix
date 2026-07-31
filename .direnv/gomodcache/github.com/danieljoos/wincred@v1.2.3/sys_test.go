//go:build windows
// +build windows

package wincred

import (
	"errors"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockProc struct {
	mock.Mock
	orig   proc
	target *proc
}

func (t *mockProc) Setup(target *proc) {
	t.target = target
	t.orig = *t.target
	*(t.target) = t
}

func (t *mockProc) TearDown() {
	*(t.target) = t.orig
}

func (t *mockProc) Call(a ...uintptr) (r1, r2 uintptr, lastErr error) {
	args := t.Called(a)
	return uintptr(args.Int(0)), uintptr(args.Int(1)), args.Error(2)
}

func TestSysCredWrite_MockFailure(t *testing.T) {
	// Mock `CreadWrite`: returns failure state and the error
	mockCredWrite := new(mockProc)
	mockCredWrite.On("Call", mock.AnythingOfType("[]uintptr")).Return(0, 0, errors.New("test error"))
	mockCredWrite.Setup(&procCredWrite)
	defer mockCredWrite.TearDown()

	// Test it:
	var err error
	assert.NotPanics(t, func() { err = sysCredWrite(new(Credential), sysCRED_TYPE_GENERIC) })
	assert.NotNil(t, err)
	assert.Equal(t, "test error", err.Error())
	mockCredWrite.AssertNumberOfCalls(t, "Call", 1)
}

func TestSysCredWrite_Mock(t *testing.T) {
	// Mock `CreadWrite`: returns success state
	mockCredWrite := new(mockProc)
	mockCredWrite.On("Call", mock.AnythingOfType("[]uintptr")).Return(1, 0, nil)
	mockCredWrite.Setup(&procCredWrite)
	defer mockCredWrite.TearDown()

	// Test it:
	var err error
	assert.NotPanics(t, func() { err = sysCredWrite(new(Credential), sysCRED_TYPE_GENERIC) })
	assert.Nil(t, err)
	mockCredWrite.AssertNumberOfCalls(t, "Call", 1)
}

func TestSysCredDelete_MockFailure(t *testing.T) {
	// Mock `CreadDelete`: returns failure state and an error
	mockCredDelete := new(mockProc)
	mockCredDelete.On("Call", mock.AnythingOfType("[]uintptr")).Return(0, 0, errors.New("test error"))
	mockCredDelete.Setup(&procCredDelete)
	defer mockCredDelete.TearDown()

	// Test it:
	var err error
	assert.NotPanics(t, func() { err = sysCredDelete(new(Credential), sysCRED_TYPE_GENERIC) })
	assert.NotNil(t, err)
	assert.Equal(t, "test error", err.Error())
	mockCredDelete.AssertNumberOfCalls(t, "Call", 1)
}

func TestSysCredDelete_Mock(t *testing.T) {
	// Mock `CreadDelete`: returns success state
	mockCredDelete := new(mockProc)
	mockCredDelete.On("Call", mock.AnythingOfType("[]uintptr")).Return(1, 0, nil)
	mockCredDelete.Setup(&procCredDelete)
	defer mockCredDelete.TearDown()

	// Test it:
	var err error
	assert.NotPanics(t, func() { err = sysCredDelete(new(Credential), sysCRED_TYPE_GENERIC) })
	assert.Nil(t, err)
	mockCredDelete.AssertNumberOfCalls(t, "Call", 1)
}

func TestCredWrite_GCSafety_WithAttributes(t *testing.T) {
	// Minimal repro for the Go 1.25 regression: we create a credential that has at least
	// one Attribute with a non-empty Value (so sysFromCredential allocates the attributes
	// slice internally). Then we force a GC after building the native struct and *before*
	// calling CredWriteW. With the old uintptr-based fields, the GC can reclaim the slice,
	// leaving dangling addresses and causing ERROR_INVALID_PARAMETER. With the fix, it’s fine.
	cred := &Credential{
		TargetName:     "Foo",
		Comment:        "Bar",
		LastWritten:    time.Now(),
		TargetAlias:    "MyAlias",
		UserName:       "Nobody",
		Persist:        PersistLocalMachine,
		CredentialBlob: []byte("secret"),
		Attributes: []CredentialAttribute{
			{Keyword: "label", Value: []byte("hello-world")},
		},
	}

	ncred := sysFromCredential(cred)
	ncred.Type = uint32(sysCRED_TYPE_GENERIC)

	// run GC a few times to gc the attributes slice.
	for i := 0; i < 5; i++ {
		runtime.GC()
	}

	// call CredWriteW - same as sysCredWrite
	ret, _, err := procCredWrite.Call(uintptr(unsafe.Pointer(ncred)), 0)
	if ret == 0 {
		t.Fatalf("CredWriteW failed: %v", err)
	}

	_ = sysCredDelete(cred, sysCRED_TYPE_GENERIC)
}
