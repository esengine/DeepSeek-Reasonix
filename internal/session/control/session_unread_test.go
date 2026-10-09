package control

import "testing"

func TestMarkSessionViewedWithoutASessionIsANoOp(t *testing.T) {
	ctrl := New(Options{})
	defer ctrl.Close()
	if err := ctrl.MarkSessionViewed(); err != nil {
		t.Fatalf("MarkSessionViewed with no session path: %v", err)
	}
}
