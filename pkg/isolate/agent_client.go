package isolate

import (
	"context"
	"time"

	"github.com/oarkflow/container/pkg/isolate/agent"
)

// AgentClient provides a simple interface to execute commands via an agent
type AgentClient struct {
	client agent.Client
}

// NewAgentClient creates a new agent client connected to a Unix socket
func NewAgentClient(socketPath string) *AgentClient {
	dialer := &agent.UnixDialer{
		Path:    socketPath,
		Timeout: 30 * time.Second,
	}
	return &AgentClient{
		client: agent.NewIPCClient(dialer),
	}
}

// Exec executes a command via the agent
func (ac *AgentClient) Exec(ctx context.Context, cmd *Command) (*Result, error) {
	req := &agent.CommandRequest{
		Path:       cmd.Path,
		Args:       cmd.Args,
		Env:        cmd.Env,
		WorkingDir: cmd.WorkingDir,
		User:       cmd.User,
		Timeout:    cmd.Timeout,
	}

	result, err := ac.client.Exec(ctx, req)
	if err != nil {
		return nil, err
	}

	return &Result{
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Duration:   result.Duration,
		StartedAt:  result.StartedAt,
		FinishedAt: result.FinishedAt,
	}, nil
}

// Close closes the agent client connection
func (ac *AgentClient) Close() error {
	if ac.client != nil {
		return ac.client.Close()
	}
	return nil
}
