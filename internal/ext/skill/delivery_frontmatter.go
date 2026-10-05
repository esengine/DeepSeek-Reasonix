// delivery_frontmatter.go — the one namespace an external profile may declare
// in, and the one it may not. Both are read from the document's structure, so
// what a field means is decided by the key it was written under rather than by
// its spelling.
package skill

import (
	"fmt"
	"strings"

	"reasonix/internal/base/frontmatter"
)

const (
	deliveryNamespace     = "delivery"
	authorityNamespace    = "authority"
	deliveryReviewReport  = "review-report"
	hostOwnedAuthorityMsg = "`authority:` is host-owned and cannot be declared by a profile. A profile says what it delivers; what the host will accept as proof is issued to it, never claimed by it. Remove the block."
)

// DeliveryError identifies a rejected profile declaration without retaining
// source values or unknown field names.
type DeliveryError string

const (
	ErrProfileAuthority DeliveryError = "profile.authority.host_owned"
	ErrDeliveryMapping  DeliveryError = "profile.delivery.mapping_required"
	ErrDeliveryField    DeliveryError = "profile.delivery.unknown_field"
	ErrDeliveryScalar   DeliveryError = "profile.delivery.scalar_required"
	ErrDeliveryReport   DeliveryError = "profile.delivery.unsupported_report"
)

func (e DeliveryError) Code() string { return string(e) }

func (e DeliveryError) Error() string {
	var message string
	switch e {
	case ErrProfileAuthority:
		message = hostOwnedAuthorityMsg
	case ErrDeliveryMapping:
		message = "`delivery:` must be a block of fields, for example:\n  delivery:\n    review-report: review"
	case ErrDeliveryField:
		message = "unknown delivery field; delivery accepts: review-report"
	case ErrDeliveryScalar:
		message = "delivery.review-report takes one value: review or security"
	case ErrDeliveryReport:
		message = "unsupported delivery.review-report; accepted: review, security"
	}
	return fmt.Sprintf("[%s] %s", e.Code(), message)
}

// deliveryFromDocument reads the external delivery contract. The namespace is
// strict: a field inside it that the host does not know is a contract the
// author believes is in force and is not, which is the failure this whole
// boundary exists to prevent. Legacy top-level keys keep their old tolerance.
func deliveryFromDocument(doc frontmatter.Document) (DeliveryContract, error) {
	if doc.Has(authorityNamespace) {
		return DeliveryContract{}, ErrProfileAuthority
	}
	value, ok := doc.Lookup(deliveryNamespace)
	if !ok {
		return DeliveryContract{}, nil
	}
	if value.Kind != frontmatter.KindMapping {
		return DeliveryContract{}, ErrDeliveryMapping
	}
	var out DeliveryContract
	for _, f := range value.Fields {
		switch f.Key {
		case deliveryReviewReport:
			kind, err := reviewReportKindOf(f.Value)
			if err != nil {
				return DeliveryContract{}, err
			}
			out.ReviewReport = kind
		default:
			return DeliveryContract{}, ErrDeliveryField
		}
	}
	return out, nil
}

func reviewReportKindOf(v frontmatter.Value) (string, error) {
	if v.Kind != frontmatter.KindScalar {
		return "", ErrDeliveryScalar
	}
	switch kind := strings.ToLower(strings.TrimSpace(v.Scalar)); kind {
	case ReviewReportReview, ReviewReportSecurity:
		return kind, nil
	default:
		return "", ErrDeliveryReport
	}
}

// misplacedDeliveryField names a delivery field written at the top level. It is
// not an alias: honoring it would put the namespace back into the flat vocabulary
// the namespace exists to leave.
func misplacedDeliveryField(doc frontmatter.Document) string {
	if doc.Has(deliveryReviewReport) {
		return deliveryReviewReport
	}
	return ""
}
