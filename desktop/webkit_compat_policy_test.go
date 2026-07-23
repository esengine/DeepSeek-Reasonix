package main

import (
	"os"
	"testing"
)

func TestConfigureWebKitRendererRecoveryForGPU(t *testing.T) {
	const key = "WEBKIT_DISABLE_DMABUF_RENDERER"
	tests := []struct {
		name      string
		safeMode  bool
		nvidiaGPU bool
		explicit  *string
		wantSet   bool
		wantValue string
	}{
		{name: "normal NVIDIA launch keeps acceleration", nvidiaGPU: true},
		{name: "safe mode without NVIDIA keeps acceleration", safeMode: true},
		{name: "safe mode NVIDIA launch disables DMA-BUF", safeMode: true, nvidiaGPU: true, wantSet: true, wantValue: "1"},
		{name: "preconfigured zero value is preserved", safeMode: true, nvidiaGPU: true, explicit: stringPointer("0"), wantSet: true, wantValue: "0"},
		{name: "preconfigured enabled value is preserved", safeMode: true, nvidiaGPU: true, explicit: stringPointer("1"), wantSet: true, wantValue: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreEnv(t, key)
			if tt.explicit != nil {
				if err := os.Setenv(key, *tt.explicit); err != nil {
					t.Fatal(err)
				}
			}

			configureWebKitRendererRecoveryForGPU(tt.safeMode, tt.nvidiaGPU)

			got, set := os.LookupEnv(key)
			if set != tt.wantSet || got != tt.wantValue {
				t.Fatalf("%s = %q, set=%v; want %q, set=%v", key, got, set, tt.wantValue, tt.wantSet)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func restoreEnv(t *testing.T, key string) {
	t.Helper()
	previous, set := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if set {
			_ = os.Setenv(key, previous)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
