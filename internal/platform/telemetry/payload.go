package telemetry

type Counter struct {
	Signal string `json:"signal"`
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

type pendingPayload struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	// Surface is empty in payloads queued before it existed; readers default it
	// to cli rather than dropping a queue an upgrade inherited.
	Surface  string    `json:"surface,omitempty"`
	Counters []Counter `json:"counters"`
}

type pingPayload struct {
	InstallID string `json:"installId"`
	Version   string `json:"version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Surface   string `json:"surface"`
}

type metricsPayload struct {
	Version  string    `json:"version"`
	OS       string    `json:"os"`
	Surface  string    `json:"surface"`
	Counters []Counter `json:"counters"`
}
