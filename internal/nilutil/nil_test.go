package nilutil

import "testing"

type sampleInterface interface {
	Do()
}

type sampleImpl struct{}

func (*sampleImpl) Do() {}

func TestIsNilDetectsTypedNilInterface(t *testing.T) {
	var p *sampleImpl
	var i sampleInterface = p

	if !IsNil(i) {
		t.Fatal("IsNil should detect typed nil interface")
	}
}

func TestIsNilRejectsConcreteValues(t *testing.T) {
	var p sampleInterface = &sampleImpl{}

	if IsNil(p) {
		t.Fatal("IsNil should not reject a concrete interface value")
	}
	if IsNil("x") {
		t.Fatal("IsNil should not reject non-nil scalar values")
	}
}
