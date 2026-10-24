package provider

import "fmt"

// ImageInputError marks request preparation, independently of a nested HTTP
// failure. Unwrap preserves cancellation and provider-specific diagnostics.
type ImageInputError struct{ Err error }

func (e *ImageInputError) Error() string { return fmt.Sprintf("image processing failed: %v", e.Err) }
func (e *ImageInputError) Unwrap() error { return e.Err }

const FailureKindImageInput = "image_input"
