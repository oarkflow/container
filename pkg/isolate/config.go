package isolate

import (
	"context"
	"io"
	"time"

	runtimectl "github.com/oarkflow/container/pkg/isolate/runtime"
)

// NetworkMode re-exports the runtime network modes so callers can stay within
// a single high-level package when configuring containers.
type NetworkMode = runtimectl.NetworkMode

// NetworkConfig re-exports the advanced runtime networking configuration.
type NetworkConfig = runtimectl.NetworkConfig

// PortForward re-exports the runtime port forwarding definition.
type PortForward = runtimectl.PortForward

// BandwidthLimit re-exports bandwidth configuration.
type BandwidthLimit = runtimectl.BandwidthLimit

// NetworkInterface re-exports the runtime NIC configuration.
type NetworkInterface = runtimectl.NetworkInterface

// NetworkInterfaceStatus re-exports the realized interface status structure.
type NetworkInterfaceStatus = runtimectl.NetworkInterfaceStatus

// InterfaceStats re-exports the runtime per-interface metrics structure.
type InterfaceStats = runtimectl.InterfaceStats

// Mount re-exports the runtime mount definition for the same reason as
// NetworkMode.
type Mount = runtimectl.Mount

// Config captures the resources and behaviors required to provision an
// isolated execution environment backed by a guest VM managed by the
// selected runtime.
type Config struct {
	Name        string
	Image       string
	CPUs        int
	Memory      int64 // bytes
	DiskSize    int64 // bytes
	NetworkMode NetworkMode
	Network     *NetworkConfig
	Mounts      []Mount
	Environment map[string]string
	WorkingDir  string
	Metadata    map[string]string
	DevMode     bool // enables host-loopback agent for local development
}

// Command represents a single guest execution request.
type Command struct {
	Path       string
	Args       []string
	Env        map[string]string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Timeout    time.Duration
	WorkingDir string
	User       string
}

// Result contains the captured command output.
type Result struct {
	ExitCode   int
	Stdout     []byte
	Stderr     []byte
	Duration   time.Duration
	StartedAt  time.Time
	FinishedAt time.Time
}

// Stream transports live stdout/stderr events alongside the eventual result.
type Stream struct {
	Stdout <-chan []byte
	Stderr <-chan []byte
	Done   <-chan *Result
	cancel context.CancelFunc
}

// Close stops the stream and releases resources.
func (s *Stream) Close() {
	if s != nil && s.cancel != nil {
		s.cancel()
	}
}

// Status mirrors the VM status from the runtime layer.
type Status struct {
	ID          string
	Name        string
	State       runtimectl.VMState
	CreatedAt   time.Time
	StartedAt   time.Time
	UpdatedAt   time.Time
	GuestIP     string
	Interfaces  []NetworkInterfaceStatus
	ResolvedIPs []string
	NetworkPlan []string
}

// Stats mirrors low-level runtime metrics in a simplified format for callers.
type Stats struct {
	CPUPercent     float64
	MemoryBytes    uint64
	DiskBytes      uint64
	NetworkRxBytes uint64
	NetworkTxBytes uint64
	Interfaces     []InterfaceStats
}
