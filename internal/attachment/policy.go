package attachment

const (
	MaxSourceBytes     = 64 << 20
	MaxSourcePixels    = 50_000_000
	DefaultMaxCount    = 20
	DefaultBatchBytes  = 200 << 20
	ViewImageMaxBytes  = 3 << 20
	ViewImageMaxPixels = 40_000_000
	VariantMaxDim      = 1568
	VariantJPEGQuality = 85
	VariantPolicyV1    = 1
	DefaultCacheBytes  = 512 << 20
	DefaultTransforms  = 2
)

// Policy is the shared budget object. Host is the final enforcer.
type Policy struct {
	MaxBytes      int64
	MaxPixels     int64
	MaxCount      int
	MaxBatchBytes int64
}

func DefaultPolicy() Policy {
	return Policy{
		MaxBytes:      MaxSourceBytes,
		MaxPixels:     MaxSourcePixels,
		MaxCount:      DefaultMaxCount,
		MaxBatchBytes: DefaultBatchBytes,
	}
}

func ViewImagePolicy() Policy {
	return Policy{
		MaxBytes:      ViewImageMaxBytes,
		MaxPixels:     ViewImageMaxPixels,
		MaxCount:      1,
		MaxBatchBytes: ViewImageMaxBytes,
	}
}

func (p Policy) withDefaults() Policy {
	if p.MaxBytes <= 0 {
		p.MaxBytes = MaxSourceBytes
	}
	if p.MaxPixels <= 0 {
		p.MaxPixels = MaxSourcePixels
	}
	if p.MaxCount <= 0 {
		p.MaxCount = DefaultMaxCount
	}
	if p.MaxBatchBytes <= 0 {
		p.MaxBatchBytes = DefaultBatchBytes
	}
	return p
}
