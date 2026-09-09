package config

import "testing"

func TestAMDCloudPreset(t *testing.T) {
	p, ok := CuratedProviderPreset("amd-gpu-cloud")
	if !ok || len(p.Entries) != 1 {
		t.Fatal("missing AMD preset")
	}
	e := p.Entries[0]
	if e.Kind != "openai" || e.BaseURL != "https://developer.amd.com.cn/radeon/api/v1" || e.Default != "DeepSeek-V4-Flash" {
		t.Fatalf("unexpected AMD route: %+v", e)
	}
	if CatalogForProviderPreset(p).BrandID != "amd" {
		t.Fatal("missing AMD brand")
	}
}
