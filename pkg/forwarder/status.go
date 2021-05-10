package forwarder

import (
	"github.com/rcrowley/go-metrics"
	"sync"
	"time"
)

type Status struct {
	InputEventCount  metrics.Meter
	InputByteCount   metrics.Meter
	OutputEventCount metrics.Meter
	OutputByteCount  metrics.Meter
	ErrorCount       metrics.Meter

	IsConnected     bool
	LastConnectTime time.Time
	StartTime       time.Time

	LastConnectError string
	ErrorTime        time.Time
	Healthy          metrics.Healthcheck

	sync.RWMutex
}

func NewStatus() *Status {
	status := Status{}
	status.InputEventCount = metrics.NewRegisteredMeter("core.input.events", metrics.DefaultRegistry)
	status.InputByteCount = metrics.NewRegisteredMeter("core.input.data", metrics.DefaultRegistry)
	status.OutputEventCount = metrics.NewRegisteredMeter("core.output.events", metrics.DefaultRegistry)
	status.OutputByteCount = metrics.NewRegisteredMeter("core.output.data", metrics.DefaultRegistry)
	status.ErrorCount = metrics.NewRegisteredMeter("errors", metrics.DefaultRegistry)
	return &status
}
