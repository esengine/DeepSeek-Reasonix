package telemetry

type Counter struct {
	Signal string `json:"signal"`
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

type pendingPayload struct {
	Version  string    `json:"version"`
	OS       string    `json:"os"`
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
	InstallID string    `json:"installId,omitempty"`
	Version   string    `json:"version"`
	OS        string    `json:"os"`
	Surface   string    `json:"surface"`
	Counters  []Counter `json:"counters"`
}
