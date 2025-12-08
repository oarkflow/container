package runtime

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/oarkflow/container/pkg/isolate/agent"
)

var (
	ErrRuntimeNotRegistered = errors.New("runtime not registered")
	ErrNoRuntimeAvailable   = errors.New("no runtime available for host")
)

// NetworkMode controls how the guest is connected to the host network stack.
type NetworkMode string

const (
	NetworkModeIsolated NetworkMode = "isolated"
	NetworkModeNAT      NetworkMode = "nat"
	NetworkModeBridge   NetworkMode = "bridge"
)

// PortProtocol enumerates supported transport layers for port forwarding.
type PortProtocol string

const (
	PortProtocolTCP PortProtocol = "tcp"
	PortProtocolUDP PortProtocol = "udp"
)

// PortForward exposes a single host<->guest port mapping.
type PortForward struct {
	Protocol    PortProtocol
	HostIP      string
	HostPort    int
	GuestPort   int
	Description string
}

// BandwidthLimit constrains network throughput in bits per second.
type BandwidthLimit struct {
	IngressBitsPerSec int64
	EgressBitsPerSec  int64
}

// NetworkInterface models an additional NIC exposed to the guest.
type NetworkInterface struct {
	Name       string
	MACAddress string
	SubnetCIDR string
	Gateway    string
	IPv4       string
	IPv6       string
	MTU        int
}

// NetworkConfig captures advanced networking configuration beyond the
// high-level mode selector.
type NetworkConfig struct {
	Mode          NetworkMode
	Hostname      string
	DNS           []string
	PortForwards  []PortForward
	Interfaces    []NetworkInterface
	Bandwidth     *BandwidthLimit
	EnableMetrics bool
}

// NetworkInterfaceStatus represents the realized state of a guest-facing
// interface and the host resources backing it.
type NetworkInterfaceStatus struct {
	Name          string
	MACAddress    string
	HostDevice    string
	Bridge        string
	Switch        string
	GuestIPv4     string
	GuestIPv6     string
	State         string
	PortForwards  []PortForward
	FirewallRules []string
	LastUpdated   time.Time
}

// InterfaceStats captures per-interface throughput metrics.
type InterfaceStats struct {
	Name      string
	RXBytes   uint64
	TXBytes   uint64
	RXPackets uint64
	TXPackets uint64
}

// MountType distinguishes mount backends.
type MountType string

const (
	MountTypeBind     MountType = "bind"
	MountTypeVolume   MountType = "volume"
	MountTypeVirtioFS MountType = "virtiofs"
)

// Mount describes a host<->guest filesystem mapping.
type Mount struct {
	Source   string
	Target   string
	Type     MountType
	ReadOnly bool
}

// VMConfig captures low-level instrumentation for each VM created by a runtime.
type VMConfig struct {
	ID          string
	Name        string
	CPUs        int
	MemoryBytes int64
	DiskSize    int64
	ImagePath   string
	KernelImage string
	InitrdPath  string
	NetworkMode NetworkMode
	Network     NetworkConfig
	Mounts      []Mount
	Environment map[string]string
	WorkingDir  string
	Metadata    map[string]string
	DevMode     bool
}

// Image contains metadata for VM images managed by a runtime.
type Image struct {
	ID          string
	Name        string
	Path        string
	Version     string
	SizeBytes   int64
	DefaultUser string
}

// ExecResult mirrors the agent command response at the runtime boundary.
type ExecResult struct {
	ExitCode   int
	Stdout     []byte
	Stderr     []byte
	Duration   time.Duration
	StartedAt  time.Time
	FinishedAt time.Time
}

// VMStats exposes lightweight performance metrics.
type VMStats struct {
	CPUPercent     float64
	MemoryBytes    uint64
	DiskBytes      uint64
	NetworkRxBytes uint64
	NetworkTxBytes uint64
	Interfaces     []InterfaceStats
}

// VMState enumerates the lifecycle phases of a guest.
type VMState string

const (
	VMStatePending VMState = "pending"
	VMStateRunning VMState = "running"
	VMStateStopped VMState = "stopped"
	VMStateDeleted VMState = "deleted"
	VMStateFailed  VMState = "failed"
)

// VMStatus bundles human friendly lifecycle data.
type VMStatus struct {
	State       VMState
	CreatedAt   time.Time
	StartedAt   time.Time
	UpdatedAt   time.Time
	GuestIP     string
	Interfaces  []NetworkInterfaceStatus
	ResolvedIPs []string
	NetworkPlan []string
}

// Runtime defines the hypervisor abstraction shared by all platforms.
type Runtime interface {
	Name() string
	Version() string
	OS() string
	Hypervisor() string
	Available() bool

	CreateVM(ctx context.Context, cfg *VMConfig) (VM, error)
	ListVMs(ctx context.Context) ([]VM, error)
	GetVM(ctx context.Context, id string) (VM, error)

	ImportImage(ctx context.Context, path string) error
	ListImages(ctx context.Context) ([]Image, error)
}

// VM is a live guest managed by a runtime implementation.
type VM interface {
	ID() string
	Config() *VMConfig
	State() VMState
	Start(ctx context.Context) error
	Stop(ctx context.Context, force bool) error
	Delete(ctx context.Context) error
	Execute(ctx context.Context, cmd *agent.CommandRequest) (*ExecResult, error)
	ExecStream(ctx context.Context, cmd *agent.CommandRequest) (*agent.CommandStream, error)
	CopyTo(ctx context.Context, reader io.Reader, dst string) error
	CopyFrom(ctx context.Context, src string, writer io.Writer) error
	Status(ctx context.Context) (*VMStatus, error)
	Stats(ctx context.Context) (*VMStats, error)
}

// Descriptor captures metadata about runtime implementations for registry usage.
type Descriptor struct {
	Name       string
	OS         string
	Hypervisor string
	Priority   int
	Notes      string
}

// Factory constructs runtime implementations on demand.
type Factory func() Runtime

type registeredRuntime struct {
	descriptor Descriptor
	factory    Factory
}

var (
	registryMu sync.RWMutex
	registry   = map[string]registeredRuntime{}
)

// Register wires a runtime factory into the global registry.
func Register(desc Descriptor, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[desc.Name] = registeredRuntime{descriptor: desc, factory: factory}
}

// AvailableRuntimes returns all runtimes compatible with the provided OS sorted
// by priority (lower value == higher preference).
func AvailableRuntimes(targetOS string) []Descriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()

	descriptors := make([]Descriptor, 0, len(registry))
	for _, entry := range registry {
		if entry.descriptor.OS == targetOS {
			descriptors = append(descriptors, entry.descriptor)
		}
	}

	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Priority < descriptors[j].Priority
	})

	return descriptors
}

// Acquire constructs a specific runtime by name.
func Acquire(name string) (Runtime, error) {
	registryMu.RLock()
	entry, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, ErrRuntimeNotRegistered
	}
	return entry.factory(), nil
}

// DefaultForHost returns the best available runtime for the current GOOS.
func DefaultForHost() (Runtime, error) {
	targetOS := runtime.GOOS
	descriptors := AvailableRuntimes(targetOS)
	if len(descriptors) == 0 {
		return nil, ErrNoRuntimeAvailable
	}

	for _, desc := range descriptors {
		rt, err := Acquire(desc.Name)
		if err != nil {
			continue
		}
		if rt.Available() {
			return rt, nil
		}
	}

	return nil, ErrNoRuntimeAvailable
}
