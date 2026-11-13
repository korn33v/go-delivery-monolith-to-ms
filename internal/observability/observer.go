package observability

// Implements the small Observer ports without importing telemetry into use cases.
func (t *Telemetry) Event(name, outcome string) { t.Business.WithLabelValues(name, outcome).Inc() }
