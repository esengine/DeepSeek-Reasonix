package attachment

import "testing"

func TestAdmissionCountAndByteBoundaries(t *testing.T) {
	svc := testService(t)
	image := opaquePNG(t, 1, 1)
	sources := make([]Source, 21)
	for i := range sources {
		sources[i] = Source{Bytes: image}
	}
	if got, err := svc.PrepareBatch(t.Context(), sources[:20]); err != nil || len(got.Items) != 20 {
		t.Fatalf("20 images: count=%d err=%v", len(got.Items), err)
	}
	if _, err := svc.PrepareBatch(t.Context(), sources); !Is(err, CodeTooMany) {
		t.Fatalf("21 images: %v", err)
	}
	// Valid PNGs may contain trailing data. Share one immutable source buffer
	// to exercise the real 200 MiB boundary without decoding giant rasters.
	raw := make([]byte, MaxSourceBytes+1)
	copy(raw, image)
	if _, err := svc.PrepareBatch(t.Context(), []Source{{Bytes: raw[:MaxSourceBytes]}}); err != nil {
		t.Fatalf("64 MiB: %v", err)
	}
	if _, err := svc.PrepareBatch(t.Context(), []Source{{Bytes: raw}}); !Is(err, CodeSize) {
		t.Fatalf("64 MiB + 1: %v", err)
	}
	batch := []Source{{Bytes: raw[:50<<20]}, {Bytes: raw[:50<<20]}, {Bytes: raw[:50<<20]}, {Bytes: raw[:50<<20]}}
	if _, err := svc.PrepareBatch(t.Context(), batch); err != nil {
		t.Fatalf("200 MiB: %v", err)
	}
	batch[3].Bytes = raw[:(50<<20)+1]
	if _, err := svc.PrepareBatch(t.Context(), batch); !Is(err, CodeBatchSize) {
		t.Fatalf("200 MiB + 1: %v", err)
	}
}
