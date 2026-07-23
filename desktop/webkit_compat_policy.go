package main

import "os"

// configureWebKitRendererRecoveryForGPU keeps the environment policy separate
// from Linux GPU detection so every platform can regression-test the decision.
// Only the Linux wrapper invokes it in production.
func configureWebKitRendererRecoveryForGPU(safeMode, nvidiaGPU bool) {
	if !safeMode || !nvidiaGPU {
		return
	}
	if _, explicitlySet := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); explicitlySet {
		return
	}
	_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
}
