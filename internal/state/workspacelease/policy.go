package workspacelease

import "time"

type Policy struct {
	Optimistic bool
	MaxWait    time.Duration
}

func (o *Owner) Policy() Policy {
	if o == nil {
		return Policy{}
	}
	return Policy{Optimistic: o.undeclaredFree, MaxWait: o.maxWait}
}
