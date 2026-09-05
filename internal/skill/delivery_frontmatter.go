// delivery_frontmatter.go — the one namespace an external profile may declare
// in, and the one it may not. Both are read from the document's structure, so
// what a field means is decided by the key it was written under rather than by
// its spelling.
package skill

import (
	"fmt"
	"strings"

	"reasonix/internal/frontmatter"
)

const (
	deliveryNamespace     = "delivery"
	authorityNamespace    = "authority"
	deliveryReviewReport  = "review-report"
	hostOwnedAuthorityMsg = "`authority:` is host-owned and cannot be declared by a profile. A profile says what it delivers; what the host will accept as proof is issued to it, never claimed by it. Remove the block."
)

// deliveryFromDocument reads the external delivery contract. The namespace is
// strict: a field inside it that the host does not know is a contract the
// author believes is in force and is not, which is the failure this whole
// boundary exists to prevent. Legacy top-level keys keep their old tolerance.
func deliveryFromDocument(doc frontmatter.Document) (DeliveryContract, error) {
	if doc.Has(authorityNamespace) {
		return DeliveryContract{}, fmt.Errorf("%s", hostOwnedAuthorityMsg)
	}
	value, ok := doc.Lookup(deliveryNamespace)
	if !ok {
		return DeliveryContract{}, nil
	}
	if value.Kind != frontmatter.KindMapping {
		return DeliveryContract{}, fmt.Errorf("`delivery:` must be a block of fields, for example:\n  delivery:\n    %s: review", deliveryReviewReport)
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
			return DeliveryContract{}, fmt.Errorf("unknown delivery field %q; delivery accepts: %s", f.Key, deliveryReviewReport)
		}
	}
	return out, nil
}

func reviewReportKindOf(v frontmatter.Value) (string, error) {
	if v.Kind != frontmatter.KindScalar {
		return "", fmt.Errorf("delivery.%s takes one value: %s or %s", deliveryReviewReport, ReviewReportReview, ReviewReportSecurity)
	}
	switch kind := strings.ToLower(strings.TrimSpace(v.Scalar)); kind {
	case ReviewReportReview, ReviewReportSecurity:
		return kind, nil
	default:
		return "", fmt.Errorf("delivery.%s = %q; accepted: %s, %s", deliveryReviewReport, v.Scalar, ReviewReportReview, ReviewReportSecurity)
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
