package store

import (
	"errors"
	"testing"
)

func TestGet(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed map[string]string
		key  string
		want string
		err  error
	}{
		{name: "present", seed: map[string]string{"a": "1"}, key: "a", want: "1"},
		{name: "missing", seed: nil, key: "a", err: ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			for k, v := range tc.seed {
				if err := s.Put(k, v); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Get(tc.key)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
