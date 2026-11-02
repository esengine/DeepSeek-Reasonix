package event

import "reasonix/internal/base/nilutil"

// UsageScopeSource captures an opaque host accounting owner. A non-nil token
// can explicitly represent work outside a goal; nil means no scope capability.
type UsageScopeSource interface {
	CaptureUsageScope() any
}

// CaptureUsageScope reads the current accounting owner without changing it.
func CaptureUsageScope(s Sink) any {
	if !nilutil.IsNil(s) {
		if source, ok := s.(UsageScopeSource); ok {
			return source.CaptureUsageScope()
		}
	}
	return nil
}

// BindUsageScope preserves the current accounting owner across asynchronous
// work. The original sink still owns quoting, audit forwarding and delivery.
func BindUsageScope(s Sink) Sink {
	scope := CaptureUsageScope(s)
	if scope == nil {
		return s
	}
	return scopedUsageSink{AuditForwarder: AuditForwarder{Inner: s}, scope: scope}
}

// UsageScope returns host-only attribution; it is not serialized with Event.
func UsageScope(e Event) any { return e.usageScope }

type scopedUsageSink struct {
	AuditForwarder
	scope any
}

func (s scopedUsageSink) CaptureUsageScope() any { return s.scope }

func (s scopedUsageSink) Emit(e Event) {
	if e.Kind == Usage && e.usageScope == nil {
		e.usageScope = s.scope
	}
	s.Inner.Emit(e)
}
