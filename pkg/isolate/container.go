package isolate

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oarkflow/container/pkg/isolate/agent"
	runtimectl "github.com/oarkflow/container/pkg/isolate/runtime"
)

// Container captures lifecycle and execution primitives for a single guest.
type Container interface {
	Create(ctx context.Context, cfg *Config) error
	Start(ctx context.Context) error
	Stop(ctx context.Context, timeout time.Duration) error
	Delete(ctx context.Context) error
	Exec(ctx context.Context, cmd *Command) (*Result, error)
	ExecStream(ctx context.Context, cmd *Command) (*Stream, error)
	Status(ctx context.Context) (*Status, error)
	Stats(ctx context.Context) (*Stats, error)
}

// containerImpl wires the high-level container API to a runtime VM.
type containerImpl struct {
	mu      sync.RWMutex
	cfg     *Config
	runtime runtimectl.Runtime
	vm      runtimectl.VM
}

func newContainer(rt runtimectl.Runtime, cfg *Config) *containerImpl {
	return &containerImpl{runtime: rt, cfg: cfg}
}

func (c *containerImpl) Create(ctx context.Context, cfg *Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.vm != nil {
		return nil
	}

	vmCfg := toVMConfig(cfg)
	vm, err := c.runtime.CreateVM(ctx, vmCfg)
	if err != nil {
		return fmt.Errorf("create vm: %w", err)
	}

	c.cfg = cfg
	c.vm = vm
	return nil
}

func (c *containerImpl) Start(ctx context.Context) error {
	c.mu.RLock()
	vm := c.vm
	c.mu.RUnlock()

	if vm == nil {
		return ErrContainerNotCreated
	}

	return vm.Start(ctx)
}

func (c *containerImpl) Stop(ctx context.Context, timeout time.Duration) error {
	c.mu.RLock()
	vm := c.vm
	c.mu.RUnlock()

	if vm == nil {
		return ErrContainerNotCreated
	}

	stopCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return vm.Stop(stopCtx, false)
}

func (c *containerImpl) Delete(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.vm == nil {
		return nil
	}

	if err := c.vm.Delete(ctx); err != nil {
		return err
	}

	c.vm = nil
	return nil
}

func (c *containerImpl) Exec(ctx context.Context, cmd *Command) (*Result, error) {
	vm, err := c.getVM()
	if err != nil {
		return nil, err
	}

	req := toCommandRequest(cmd)
	execResult, err := vm.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	return &Result{
		ExitCode:   execResult.ExitCode,
		Stdout:     append([]byte(nil), execResult.Stdout...),
		Stderr:     append([]byte(nil), execResult.Stderr...),
		Duration:   execResult.Duration,
		StartedAt:  execResult.StartedAt,
		FinishedAt: execResult.FinishedAt,
	}, nil
}

func (c *containerImpl) ExecStream(ctx context.Context, cmd *Command) (*Stream, error) {
	vm, err := c.getVM()
	if err != nil {
		return nil, err
	}

	req := toCommandRequest(cmd)
	agentStream, err := vm.ExecStream(ctx, req)
	if err != nil {
		return nil, err
	}

	done := make(chan *Result, 1)

	go func() {
		res := <-agentStream.Done
		if res == nil {
			done <- nil
			return
		}
		done <- &Result{
			ExitCode:   res.ExitCode,
			Stdout:     append([]byte(nil), res.Stdout...),
			Stderr:     append([]byte(nil), res.Stderr...),
			Duration:   res.Duration,
			StartedAt:  res.StartedAt,
			FinishedAt: res.FinishedAt,
		}
	}()

	return &Stream{
		Stdout: agentStream.Stdout,
		Stderr: agentStream.Stderr,
		Done:   done,
		cancel: agentStream.Cancel,
	}, nil
}

func (c *containerImpl) Status(ctx context.Context) (*Status, error) {
	vm, err := c.getVM()
	if err != nil {
		return nil, err
	}

	vmStatus, err := vm.Status(ctx)
	if err != nil {
		return nil, err
	}

	return &Status{
		ID:          vm.ID(),
		Name:        c.cfg.Name,
		State:       vmStatus.State,
		CreatedAt:   vmStatus.CreatedAt,
		StartedAt:   vmStatus.StartedAt,
		UpdatedAt:   vmStatus.UpdatedAt,
		GuestIP:     vmStatus.GuestIP,
		Interfaces:  append([]runtimectl.NetworkInterfaceStatus(nil), vmStatus.Interfaces...),
		ResolvedIPs: append([]string(nil), vmStatus.ResolvedIPs...),
		NetworkPlan: append([]string(nil), vmStatus.NetworkPlan...),
	}, nil
}

func (c *containerImpl) Stats(ctx context.Context) (*Stats, error) {
	vm, err := c.getVM()
	if err != nil {
		return nil, err
	}

	vmStats, err := vm.Stats(ctx)
	if err != nil {
		return nil, err
	}

	return &Stats{
		CPUPercent:     vmStats.CPUPercent,
		MemoryBytes:    vmStats.MemoryBytes,
		DiskBytes:      vmStats.DiskBytes,
		NetworkRxBytes: vmStats.NetworkRxBytes,
		NetworkTxBytes: vmStats.NetworkTxBytes,
		Interfaces:     append([]runtimectl.InterfaceStats(nil), vmStats.Interfaces...),
	}, nil
}

func (c *containerImpl) getVM() (runtimectl.VM, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vm == nil {
		return nil, ErrContainerNotCreated
	}
	return c.vm, nil
}

func toVMConfig(cfg *Config) *runtimectl.VMConfig {
	if cfg == nil {
		return &runtimectl.VMConfig{}
	}

	env := make(map[string]string, len(cfg.Environment))
	for k, v := range cfg.Environment {
		env[k] = v
	}

	mounts := make([]runtimectl.Mount, len(cfg.Mounts))
	copy(mounts, cfg.Mounts)

	metadata := make(map[string]string, len(cfg.Metadata))
	for k, v := range cfg.Metadata {
		metadata[k] = v
	}

	return &runtimectl.VMConfig{
		Name:        cfg.Name,
		CPUs:        cfg.CPUs,
		MemoryBytes: cfg.Memory,
		DiskSize:    cfg.DiskSize,
		ImagePath:   cfg.Image,
		NetworkMode: cfg.NetworkMode,
		Network:     toRuntimeNetworkConfig(cfg),
		Mounts:      mounts,
		Environment: env,
		Metadata:    metadata,
		WorkingDir:  cfg.WorkingDir,
		DevMode:     cfg.DevMode,
	}
}

func toRuntimeNetworkConfig(cfg *Config) runtimectl.NetworkConfig {
	if cfg == nil {
		return runtimectl.NetworkConfig{}
	}
	if cfg.Network == nil {
		return runtimectl.NetworkConfig{Mode: cfg.NetworkMode}
	}
	src := cfg.Network
	copyDNS := append([]string(nil), src.DNS...)
	portForwards := make([]runtimectl.PortForward, len(src.PortForwards))
	copy(portForwards, src.PortForwards)
	interfaces := make([]runtimectl.NetworkInterface, len(src.Interfaces))
	copy(interfaces, src.Interfaces)
	var bandwidth *runtimectl.BandwidthLimit
	if src.Bandwidth != nil {
		bw := *src.Bandwidth
		bandwidth = &bw
	}
	return runtimectl.NetworkConfig{
		Mode:          src.Mode,
		Hostname:      src.Hostname,
		DNS:           copyDNS,
		PortForwards:  portForwards,
		Interfaces:    interfaces,
		Bandwidth:     bandwidth,
		EnableMetrics: src.EnableMetrics,
	}
}

func toCommandRequest(cmd *Command) *agent.CommandRequest {
	if cmd == nil {
		return &agent.CommandRequest{}
	}

	env := make(map[string]string, len(cmd.Env))
	for k, v := range cmd.Env {
		env[k] = v
	}

	stdout := cmd.Stdout
	if stdout == nil {
		stdout = &bytes.Buffer{}
	}

	stderr := cmd.Stderr
	if stderr == nil {
		stderr = &bytes.Buffer{}
	}

	return &agent.CommandRequest{
		Path:       cmd.Path,
		Args:       append([]string(nil), cmd.Args...),
		Env:        env,
		Stdin:      cmd.Stdin,
		Stdout:     stdout,
		Stderr:     stderr,
		Timeout:    cmd.Timeout,
		WorkingDir: cmd.WorkingDir,
		User:       cmd.User,
	}
}
