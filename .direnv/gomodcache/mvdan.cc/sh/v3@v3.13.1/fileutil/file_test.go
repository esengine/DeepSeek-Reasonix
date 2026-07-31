// Copyright (c) 2025, Ville Skyttä <ville.skytta@iki.fi>
// See LICENSE for licensing information

package fileutil

import (
	"strings"
	"testing"
)

func TestShebang(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   []byte
		want string
	}{
		{
			in:   []byte("#!/usr/bin/env bash"),
			want: "bash",
		},
		{
			in:   []byte("#!/bin/bash"),
			want: "bash",
		},
		{
			in:   []byte("#!foo bar"),
			want: "",
		},
		{
			in:   []byte("#!/bin/zsh"),
			want: "zsh",
		},
		{
			in:   []byte("#! /bin/zsh true"),
			want: "zsh",
		},
		{
			in:   []byte("#!  /bin/zsh"),
			want: "zsh",
		},
		{
			in:   []byte("#!\t/bin/zsh"),
			want: "zsh",
		},
		{
			in:   []byte("#!\f/bin/zsh"),
			want: "",
		},
	}

	for _, test := range tests {
		name := strings.ReplaceAll(strings.ReplaceAll(string(test.in), "\f", "\\f"), "\t", "\\t")
		t.Run(name, func(t *testing.T) {
			if got := Shebang(test.in); got != test.want {
				t.Fatalf("want %q, got %q", test.want, got)
			}
		})
	}
}
