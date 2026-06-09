package codegraph

// releaseAssetSHA256 holds the upstream SHA256SUMS values for Version. These
// are baked into the reasonix binary so downloaded CodeGraph archives are
// verified against trusted metadata instead of trusting whichever mirror served
// the bytes.
var releaseAssetSHA256 = map[string]string{
	"codegraph-darwin-arm64.tar.gz": "83a3b90bc334ab2a34240e29e9fab7ff273ab6381794aa6f3cba428397c916b5",
	"codegraph-darwin-x64.tar.gz":   "74d2331161a317fa6164285a61ec480ce7893be46c1677a4b5a2932e35586b9d",
	"codegraph-linux-arm64.tar.gz":  "7b4225f90ca5285cccfec099323129348c2753bcbc9910281f9b61db88fa5f37",
	"codegraph-linux-x64.tar.gz":    "61805e3c9b4052db53c71241b800859095fea4f2cbd2a1844a6c2b9594b9f84a",
	"codegraph-win32-arm64.zip":     "c728ada3d42701213dde26d8e94ded3ed1c7d4b568124210649ce8f9f938a31a",
	"codegraph-win32-x64.zip":       "a5571d3ee54cc1caac76bf09e0f7cb350fc4dd6788a5437217eac33b71fa7a15",
}
