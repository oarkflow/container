package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oarkflow/container/pkg/isolate/agent"
)

var errAgentUnavailable = errors.New("guest agent uninitialized for this VM")

// detectBinary checks if any of the provided executables are present on the
// host. The first match is returned, otherwise an empty string is produced.
func detectBinary(names ...string) string {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}

type stubRuntime struct {
	desc        Descriptor
	binary      string
	vms         map[string]*stubVM
	mu          sync.RWMutex
	versionInfo string
}

func newStubRuntime(desc Descriptor, binaryNames ...string) *stubRuntime {
	return &stubRuntime{
		desc:        desc,
		binary:      detectBinary(binaryNames...),
		vms:         make(map[string]*stubVM),
		versionInfo: "0.1.0-stub",
	}
}

func (s *stubRuntime) Name() string       { return s.desc.Name }
func (s *stubRuntime) Version() string    { return s.versionInfo }
func (s *stubRuntime) OS() string         { return s.desc.OS }
func (s *stubRuntime) Hypervisor() string { return s.desc.Hypervisor }
func (s *stubRuntime) Available() bool {
	if len(s.desc.Hypervisor) == 0 {
		return true
	}
	// If no binary hint is provided we assume the runtime is available.
	if s.binary == "" {
		return true
	}
	return true
}

func (s *stubRuntime) CreateVM(ctx context.Context, cfg *VMConfig) (VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cfg == nil {
		return nil, fmt.Errorf("vm config is required")
	}

	id := cfg.ID
	if id == "" {
		id = fmt.Sprintf("%s-%d", s.desc.Name, atomic.AddUint64(&vmCounter, 1))
	}

	if _, exists := s.vms[id]; exists {
		return nil, fmt.Errorf("vm %s already exists", id)
	}

	cfgCopy := *cfg
	guestIP, ifaceStatus, resolvedIPs, plan := synthesizeNetworkMetadata(&cfgCopy)
	vm := &stubVM{
		id:                 id,
		cfg:                &cfgCopy,
		runtime:            s,
		state:              VMStateStopped,
		agent:              selectAgentClient(&cfgCopy),
		guestIP:            guestIP,
		interfaceTemplates: ifaceStatus,
		resolvedIPs:        resolvedIPs,
		networkPlan:        plan,
	}

	s.vms[id] = vm
	return vm, nil
}

func (s *stubRuntime) ListVMs(ctx context.Context) ([]VM, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vms := make([]VM, 0, len(s.vms))
	for _, vm := range s.vms {
		vms = append(vms, vm)
	}
	return vms, nil
}

func (s *stubRuntime) GetVM(ctx context.Context, id string) (VM, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vm, ok := s.vms[id]
	if !ok {
		return nil, fmt.Errorf("vm %s not found", id)
	}
	return vm, nil
}

func (s *stubRuntime) ImportImage(ctx context.Context, path string) error {
	return fmt.Errorf("%s runtime does not manage images (stub)", s.Name())
}

func (s *stubRuntime) ListImages(ctx context.Context) ([]Image, error) {
	return nil, nil
}

type stubVM struct {
	id      string
	cfg     *VMConfig
	runtime *stubRuntime
	agent   agent.Client

	mu                 sync.RWMutex
	state              VMState
	createdAt          time.Time
	startedAt          time.Time
	updatedAt          time.Time
	guestIP            string
	interfaceTemplates []NetworkInterfaceStatus
	resolvedIPs        []string
	networkPlan        []string
}

var vmCounter uint64

func (v *stubVM) ID() string        { return v.id }
func (v *stubVM) Config() *VMConfig { return v.cfg }
func (v *stubVM) State() VMState {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.state
}

func (v *stubVM) Start(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.state = VMStateRunning
	if v.createdAt.IsZero() {
		v.createdAt = time.Now()
	}
	v.startedAt = time.Now()
	v.updatedAt = time.Now()
	return nil
}

func (v *stubVM) Stop(ctx context.Context, force bool) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state = VMStateStopped
	v.updatedAt = time.Now()
	return nil
}

func (v *stubVM) Delete(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state = VMStateDeleted
	v.updatedAt = time.Now()

	v.runtime.mu.Lock()
	delete(v.runtime.vms, v.id)
	v.runtime.mu.Unlock()

	return nil
}

func (v *stubVM) Execute(ctx context.Context, cmd *agent.CommandRequest) (*ExecResult, error) {
	if v.agent == nil {
		return nil, errAgentUnavailable
	}
	result, err := v.agent.Exec(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return &ExecResult{
		ExitCode:   result.ExitCode,
		Stdout:     append([]byte(nil), result.Stdout...),
		Stderr:     append([]byte(nil), result.Stderr...),
		Duration:   result.Duration,
		StartedAt:  result.StartedAt,
		FinishedAt: result.FinishedAt,
	}, nil
}

func (v *stubVM) ExecStream(ctx context.Context, cmd *agent.CommandRequest) (*agent.CommandStream, error) {
	if v.agent == nil {
		return nil, errAgentUnavailable
	}
	return v.agent.ExecStream(ctx, cmd)
}

func (v *stubVM) CopyTo(ctx context.Context, reader io.Reader, dst string) error {
	return v.agent.CopyTo(ctx, reader, dst)
}

func (v *stubVM) CopyFrom(ctx context.Context, src string, writer io.Writer) error {
	return v.agent.CopyFrom(ctx, src, writer)
}

func (v *stubVM) Status(ctx context.Context) (*VMStatus, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return &VMStatus{
		State:       v.state,
		CreatedAt:   v.createdAt,
		StartedAt:   v.startedAt,
		UpdatedAt:   v.updatedAt,
		GuestIP:     v.guestIP,
		Interfaces:  stampInterfaceStatus(v.interfaceTemplates),
		ResolvedIPs: append([]string(nil), v.resolvedIPs...),
		NetworkPlan: append([]string(nil), v.networkPlan...),
	}, nil
}

func (v *stubVM) Stats(ctx context.Context) (*VMStats, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	ifaceStats := make([]InterfaceStats, len(v.interfaceTemplates))
	var totalRx, totalTx uint64
	for i, iface := range v.interfaceTemplates {
		multiplier := uint64(i + 1)
		rx := 1_048_576 * multiplier // 1 MiB per interface sample
		tx := 786_432 * multiplier   // 768 KiB per interface sample
		ifaceStats[i] = InterfaceStats{
			Name:      iface.Name,
			RXBytes:   rx,
			TXBytes:   tx,
			RXPackets: 2048 * multiplier,
			TXPackets: 1536 * multiplier,
		}
		totalRx += rx
		totalTx += tx
	}

	var memoryBytes, diskBytes uint64
	if v.cfg != nil {
		memoryBytes = approxUsage(v.cfg.MemoryBytes, 2)
		diskBytes = approxUsage(v.cfg.DiskSize, 4)
	}

	return &VMStats{
		CPUPercent:     v.estimateCPUUsageLocked(),
		MemoryBytes:    memoryBytes,
		DiskBytes:      diskBytes,
		NetworkRxBytes: totalRx,
		NetworkTxBytes: totalTx,
		Interfaces:     ifaceStats,
	}, nil
}

func selectAgentClient(cfg *VMConfig) agent.Client {
	if cfg == nil {
		return agent.NewNopClient()
	}
	if meta := cfg.Metadata; meta != nil {
		if path := meta["agent.unix"]; path != "" {
			return agent.NewIPCClient(&agent.UnixDialer{Path: path})
		}
		cidStr := meta["agent.vsock.cid"]
		portStr := meta["agent.vsock.port"]
		if cidStr != "" && portStr != "" {
			cid, errCID := strconv.ParseUint(cidStr, 10, 32)
			port, errPort := strconv.ParseUint(portStr, 10, 32)
			if errCID == nil && errPort == nil {
				return agent.NewIPCClient(&agent.VsockDialer{CID: uint32(cid), Port: uint32(port)})
			}
		}
	}
	if cfg.DevMode {
		return agent.NewLoopbackClient(cfg.Environment)
	}
	return agent.NewNopClient()
}

func stampInterfaceStatus(templates []NetworkInterfaceStatus) []NetworkInterfaceStatus {
	if len(templates) == 0 {
		return nil
	}
	out := make([]NetworkInterfaceStatus, len(templates))
	now := time.Now()
	for i, tmpl := range templates {
		copy := tmpl
		copy.LastUpdated = now
		out[i] = copy
	}
	return out
}

func synthesizeNetworkMetadata(cfg *VMConfig) (string, []NetworkInterfaceStatus, []string, []string) {
	if cfg == nil {
		defaultIface := NetworkInterface{
			Name:       "eth0",
			SubnetCIDR: "10.0.0.0/24",
			Gateway:    "10.0.0.1",
			IPv4:       "10.0.0.2",
			IPv6:       "fd00::2",
			MTU:        1500,
		}
		status := NetworkInterfaceStatus{
			Name:         defaultIface.Name,
			MACAddress:   "02:00:00:00:00:02",
			HostDevice:   "vm-eth0",
			Bridge:       bridgeName(NetworkModeNAT),
			Switch:       switchName(NetworkModeNAT),
			GuestIPv4:    defaultIface.IPv4,
			GuestIPv6:    defaultIface.IPv6,
			State:        "up",
			PortForwards: nil,
			FirewallRules: []string{
				"allow egress via nat",
				"allow established ingress",
			},
		}
		resolved := dedupeStrings([]string{defaultIface.IPv4, defaultIface.IPv6})
		plan := buildNetworkPlan(NetworkModeNAT, &NetworkConfig{}, 1)
		return defaultIface.IPv4, []NetworkInterfaceStatus{status}, resolved, plan
	}

	netCfg := cfg.Network
	mode := netCfg.Mode
	if mode == "" {
		mode = cfg.NetworkMode
	}
	if mode == "" {
		mode = NetworkModeNAT
	}

	interfaces := ensureInterfaces(&netCfg)
	statuses := make([]NetworkInterfaceStatus, 0, len(interfaces))
	resolved := make([]string, 0, len(interfaces)*2)
	hostName := cfg.Name
	if hostName == "" {
		hostName = "vm"
	}
	for idx, iface := range interfaces {
		name := iface.Name
		if name == "" {
			name = fmt.Sprintf("eth%d", idx)
		}
		mac := iface.MACAddress
		if mac == "" {
			mac = fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", idx, idx+1, idx+2, idx+3)
		}
		ipv4 := iface.IPv4
		if ipv4 == "" {
			ipv4 = defaultIPv4(idx)
		}
		ipv6 := iface.IPv6
		if ipv6 == "" {
			ipv6 = defaultIPv6(idx)
		}
		status := NetworkInterfaceStatus{
			Name:         name,
			MACAddress:   mac,
			HostDevice:   fmt.Sprintf("%s-%s", hostName, name),
			Bridge:       bridgeName(mode),
			Switch:       switchName(mode),
			GuestIPv4:    ipv4,
			GuestIPv6:    ipv6,
			State:        "up",
			PortForwards: append([]PortForward(nil), netCfg.PortForwards...),
			FirewallRules: []string{
				fmt.Sprintf("allow egress via %s", mode),
				"allow established ingress",
			},
		}
		statuses = append(statuses, status)
		if ipv4 != "" {
			resolved = append(resolved, ipv4)
		}
		if ipv6 != "" {
			resolved = append(resolved, ipv6)
		}
	}

	guestIP := ""
	if len(statuses) > 0 {
		if statuses[0].GuestIPv4 != "" {
			guestIP = statuses[0].GuestIPv4
		} else {
			guestIP = statuses[0].GuestIPv6
		}
	}

	plan := buildNetworkPlan(mode, &netCfg, len(statuses))

	return guestIP, statuses, dedupeStrings(resolved), plan
}

func ensureInterfaces(cfg *NetworkConfig) []NetworkInterface {
	if cfg == nil || len(cfg.Interfaces) == 0 {
		return []NetworkInterface{defaultInterfaceDefinition()}
	}
	interfaces := make([]NetworkInterface, len(cfg.Interfaces))
	copy(interfaces, cfg.Interfaces)
	return interfaces
}

func defaultInterfaceDefinition() NetworkInterface {
	return NetworkInterface{
		Name:       "eth0",
		SubnetCIDR: "10.0.0.0/24",
		Gateway:    "10.0.0.1",
		IPv4:       "10.0.0.2",
		IPv6:       "fd00::2",
		MTU:        1500,
	}
}

func bridgeName(mode NetworkMode) string {
	switch mode {
	case NetworkModeBridge:
		return "br0"
	case NetworkModeNAT:
		return "nat0"
	default:
		return ""
	}
}

func switchName(mode NetworkMode) string {
	switch mode {
	case NetworkModeIsolated:
		return "vsw-isolated"
	case NetworkModeNAT:
		return "vsw-nat"
	case NetworkModeBridge:
		return "vsw-bridge"
	default:
		return ""
	}
}

func buildNetworkPlan(mode NetworkMode, cfg *NetworkConfig, ifaceCount int) []string {
	plan := []string{fmt.Sprintf("mode=%s", mode), fmt.Sprintf("interfaces=%d", ifaceCount)}
	if cfg == nil {
		return plan
	}
	if cfg.Hostname != "" {
		plan = append(plan, fmt.Sprintf("hostname=%s", cfg.Hostname))
	}
	if len(cfg.DNS) > 0 {
		plan = append(plan, fmt.Sprintf("dns=%s", strings.Join(cfg.DNS, ",")))
	}
	for _, pf := range cfg.PortForwards {
		proto := pf.Protocol
		if proto == "" {
			proto = PortProtocolTCP
		}
		hostIP := pf.HostIP
		if hostIP == "" {
			hostIP = "0.0.0.0"
		}
		plan = append(plan, fmt.Sprintf("forward %s:%d -> %d/%s", hostIP, pf.HostPort, pf.GuestPort, proto))
	}
	if cfg.Bandwidth != nil {
		plan = append(plan, fmt.Sprintf("bandwidth ingress=%dbps egress=%dbps", cfg.Bandwidth.IngressBitsPerSec, cfg.Bandwidth.EgressBitsPerSec))
	}
	if cfg.EnableMetrics {
		plan = append(plan, "metrics=enabled")
	}
	return plan
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}

func defaultIPv4(idx int) string {
	return fmt.Sprintf("10.20.%d.%d", idx, 10+idx)
}

func defaultIPv6(idx int) string {
	return fmt.Sprintf("fd00::%x", idx+2)
}

func approxUsage(total int64, divisor int64) uint64 {
	if total <= 0 || divisor <= 0 {
		return 0
	}
	value := total / divisor
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func (v *stubVM) estimateCPUUsageLocked() float64 {
	if v.state != VMStateRunning {
		return 0
	}
	cpus := 1
	if v.cfg != nil && v.cfg.CPUs > 0 {
		cpus = v.cfg.CPUs
	}
	return 5.0 + float64(cpus)*1.5 + float64(len(v.interfaceTemplates))
}
