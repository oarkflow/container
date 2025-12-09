package isolate

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/oarkflow/container/pkg/isolate/agent"
)

// AgentManager manages the lifecycle of a local agent daemon
type AgentManager struct {
	socketPath string
	rootDir    string
	cmd        *exec.Cmd
	mu         sync.Mutex
	running    bool
}

// NewAgentManager creates a new agent manager
func NewAgentManager(socketPath, rootDir string) *AgentManager {
	return &AgentManager{
		socketPath: socketPath,
		rootDir:    rootDir,
	}
}

// Start starts the agent daemon if not already running
func (am *AgentManager) Start(ctx context.Context) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.running {
		return nil
	}

	// Check if socket already exists and is active
	if am.isAgentRunning() {
		am.running = true
		return nil
	}

	// Remove stale socket
	_ = os.Remove(am.socketPath)

	// Ensure directory exists
	socketDir := filepath.Dir(am.socketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	// Find agentd binary or use go run
	agentCmd := am.findAgentCommand()

	// Build command
	args := []string{"-unix", am.socketPath}
	if am.rootDir != "" {
		args = append(args, "-root", am.rootDir)

		// Add --no-chroot if not running as root
		// This allows agent to start in development mode without sudo
		if os.Geteuid() != 0 {
			args = append(args, "--no-chroot")
		}
	}

	am.cmd = exec.Command(agentCmd[0], append(agentCmd[1:], args...)...)
	am.cmd.Stdout = os.Stderr
	am.cmd.Stderr = os.Stderr

	// Set process group so we can kill all child processes
	if am.cmd.SysProcAttr == nil {
		am.cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	am.cmd.SysProcAttr.Setpgid = true

	if err := am.cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}

	// Wait for socket to be ready
	if err := am.waitForSocket(5 * time.Second); err != nil {
		_ = am.cmd.Process.Kill()
		return err
	}

	am.running = true

	// Monitor process in background
	go func() {
		_ = am.cmd.Wait()
		am.mu.Lock()
		am.running = false
		am.mu.Unlock()
	}()

	return nil
}

// Stop stops the agent daemon
func (am *AgentManager) Stop() error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if !am.running || am.cmd == nil || am.cmd.Process == nil {
		return nil
	}

	// Kill the entire process group (handles go run spawned processes)
	pgid, err := syscall.Getpgid(am.cmd.Process.Pid)
	if err == nil {
		// Kill the process group
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		// Fallback to killing just the main process
		if err := am.cmd.Process.Signal(os.Interrupt); err != nil {
			_ = am.cmd.Process.Kill()
		}
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- am.cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		// Force kill if it didn't stop gracefully
		if pgid, err := syscall.Getpgid(am.cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = am.cmd.Process.Kill()
		}
		<-done // Wait for it to actually die
	}

	am.running = false
	_ = os.Remove(am.socketPath)

	return nil
}

// isAgentRunning checks if an agent is already running on the socket
func (am *AgentManager) isAgentRunning() bool {
	conn, err := net.DialTimeout("unix", am.socketPath, time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()

	// Try to ping the agent
	client := agent.NewIPCClient(&agent.UnixDialer{Path: am.socketPath, Timeout: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return client.Ping(ctx) == nil
}

// waitForSocket waits for the socket to become available
func (am *AgentManager) waitForSocket(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(am.socketPath); err == nil {
			// Socket exists, try to connect
			if am.isAgentRunning() {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("agent socket not ready after %v", timeout)
}

// findAgentCommand finds the agentd binary or falls back to go run
func (am *AgentManager) findAgentCommand() []string {
	// Try to find agentd binary
	if path, err := exec.LookPath("agentd"); err == nil {
		return []string{path}
	}

	// Check if we're in the project directory
	if _, err := os.Stat("cmd/agentd/main.go"); err == nil {
		return []string{"go", "run", "./cmd/agentd/main.go"}
	}

	// Try relative to current executable
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		agentPath := filepath.Join(exeDir, "agentd")
		if _, err := os.Stat(agentPath); err == nil {
			return []string{agentPath}
		}
	}

	// Fallback to go run with relative path
	return []string{"go", "run", "./cmd/agentd/main.go"}
}

// GetSocketPath returns the socket path
func (am *AgentManager) GetSocketPath() string {
	return am.socketPath
}

// IsRunning returns whether the agent is running
func (am *AgentManager) IsRunning() bool {
	am.mu.Lock()
	defer am.mu.Unlock()
	return am.running
}
