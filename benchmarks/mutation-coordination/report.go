package main

import "slices"

type percentiles struct {
	MedianMS float64 `json:"median_ms"`
	P95MS    float64 `json:"p95_ms"`
}

type result struct {
	Samples               []measurement `json:"samples"`
	Wall                  percentiles   `json:"wall"`
	Wait                  percentiles   `json:"acquisition"`
	Held                  percentiles   `json:"api_owned"`
	Executing             percentiles   `json:"executing"`
	Inference             percentiles   `json:"simulated_inference"`
	Conflicts             int           `json:"conflicts"`
	Rescheduled           int           `json:"rescheduled"`
	ResourceHeldLower     percentiles   `json:"resource_held_lower"`
	ResourceHeldUpper     percentiles   `json:"resource_held_upper"`
	RegistryHeld          percentiles   `json:"registry_held"`
	Preparation           percentiles   `json:"preparation"`
	InferenceWithResource percentiles   `json:"inference_with_resource"`
	ResourceWait          percentiles   `json:"resource_wait"`
}

func (r *result) summarize() {
	r.Wall = summarize(r.Samples, func(m measurement) int64 { return m.WallNS })
	r.Wait = summarize(r.Samples, func(m measurement) int64 { return m.WaitNS })
	r.Held = summarize(r.Samples, func(m measurement) int64 { return m.HeldNS })
	r.Executing = summarize(r.Samples, func(m measurement) int64 { return m.ExecutingNS })
	r.Inference = summarize(r.Samples, func(m measurement) int64 { return m.InferenceNS })
	r.ResourceHeldLower = summarize(r.Samples, func(m measurement) int64 { return m.ResourceHeldLowerNS })
	r.ResourceHeldUpper = summarize(r.Samples, func(m measurement) int64 { return m.ResourceHeldUpperNS })
	r.RegistryHeld = summarize(r.Samples, func(m measurement) int64 { return m.RegistryHeldNS })
	r.Preparation = summarize(r.Samples, func(m measurement) int64 { return m.PreparationNS })
	r.InferenceWithResource = summarize(r.Samples, func(m measurement) int64 { return m.InferenceWithResourceNS })
	r.ResourceWait = summarize(r.Samples, func(m measurement) int64 { return m.ResourceWaitNS })
	for _, sample := range r.Samples {
		r.Conflicts += sample.Conflicts
		r.Rescheduled += sample.Rescheduled
	}
}

func summarize(samples []measurement, value func(measurement) int64) percentiles {
	values := make([]int64, len(samples))
	for i, sample := range samples {
		values[i] = value(sample)
	}
	slices.Sort(values)
	mid := float64(values[len(values)/2])
	if len(values)%2 == 0 {
		mid = float64(values[len(values)/2-1])/2 + mid/2
	}
	p95 := (len(values)*95+99)/100 - 1
	return percentiles{MedianMS: mid / 1e6, P95MS: float64(values[p95]) / 1e6}
}

func (m *measurement) add(child measurement) {
	m.WaitNS += child.WaitNS
	m.HeldNS += child.HeldNS
	m.ExecutingNS += child.ExecutingNS
	m.InferenceNS += child.InferenceNS
	m.Conflicts += child.Conflicts
	m.Rescheduled += child.Rescheduled
	m.ResourceHeldLowerNS += child.ResourceHeldLowerNS
	m.ResourceHeldUpperNS += child.ResourceHeldUpperNS
	m.RegistryHeldNS += child.RegistryHeldNS
	m.ResourceWaitNS += child.ResourceWaitNS
	m.PreparationNS += child.PreparationNS
	m.InferenceWithResourceNS += child.InferenceWithResourceNS
	m.Traces = append(m.Traces, child.Traces...)
}
