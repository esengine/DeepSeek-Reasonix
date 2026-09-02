package provider

// TemperaturePtr wraps v in a pointer so callers that explicitly want a
// specific temperature, including 0 for deterministic output, can distinguish
// that intent from "not set, use the provider default".
func TemperaturePtr(v float64) *float64 { return &v }

// OptionalTemperature returns nil when v is zero, matching the historical
// config behavior where 0 meant "not configured", and a pointer otherwise.
func OptionalTemperature(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}
