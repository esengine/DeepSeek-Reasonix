package chartspec

const (
	SpecVersion = 1

	MaxSpecBytes  = 256 << 10
	MaxRows       = 5000
	MaxColumns    = 32
	MaxSeries     = 12
	MaxPoints     = 2000
	MaxMarks      = 8
	MaxLabelRunes = 64

	maxSafeInt = 1 << 53
)
