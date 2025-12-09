package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LoopbackClient executes commands directly on the host for development and
// testing when an actual guest agent is not yet available. This should never be
// used in production but provides a convenient feedback loop.
type LoopbackClient struct {
	baseEnv map[string]string
}

// NewLoopbackClient constructs a loopback agent.
func NewLoopbackClient(baseEnv map[string]string) Client {
	env := make(map[string]string, len(baseEnv))
	for k, v := range baseEnv {
		env[k] = v
	}
	return &LoopbackClient{baseEnv: env}
}

func (l *LoopbackClient) Ping(ctx context.Context) error { return nil }

func (l *LoopbackClient) Exec(ctx context.Context, cmd *CommandRequest) (*CommandResult, error) {
	start := time.Now()

	// Validate working directory to prevent path traversal
	if cmd.WorkingDir != "" {
		if err := validateWorkingDir(cmd); err != nil {
			return nil, fmt.Errorf("security violation: %w", err)
		}
	}

	command := exec.CommandContext(ctx, cmd.Path, cmd.Args...)
	command.Env = flattenEnv(l.baseEnv, cmd.Env)
	command.Dir = cmd.WorkingDir

	if cmd.Stdin != nil {
		command.Stdin = cmd.Stdin
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := command.Start(); err != nil {
		return nil, err
	}

	stdoutBytes, err := io.ReadAll(stdout)
	if err != nil {
		return nil, err
	}
	stderrBytes, err := io.ReadAll(stderr)
	if err != nil {
		return nil, err
	}

	if err := command.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &CommandResult{
				ExitCode:   exitErr.ExitCode(),
				Stdout:     stdoutBytes,
				Stderr:     stderrBytes,
				Duration:   time.Since(start),
				StartedAt:  start,
				FinishedAt: time.Now(),
			}, nil
		}
		return nil, err
	}

	return &CommandResult{
		ExitCode:   0,
		Stdout:     stdoutBytes,
		Stderr:     stderrBytes,
		Duration:   time.Since(start),
		StartedAt:  start,
		FinishedAt: time.Now(),
	}, nil
}

func (l *LoopbackClient) ExecStream(ctx context.Context, cmd *CommandRequest) (*CommandStream, error) {
	// Validate working directory to prevent path traversal
	if cmd.WorkingDir != "" {
		if err := validateWorkingDir(cmd); err != nil {
			return nil, fmt.Errorf("security violation: %w", err)
		}
	}

	command := exec.CommandContext(ctx, cmd.Path, cmd.Args...)
	command.Env = flattenEnv(l.baseEnv, cmd.Env)
	command.Dir = cmd.WorkingDir
	command.Stdin = cmd.Stdin

	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := command.Start(); err != nil {
		return nil, err
	}

	stdoutCh := make(chan []byte, 1)
	stderrCh := make(chan []byte, 1)
	doneCh := make(chan *CommandResult, 1)

	ctx, cancel := context.WithCancel(ctx)

	wg := sync.WaitGroup{}
	wg.Add(2)

	go streamPipe(ctx, &wg, stdoutPipe, stdoutCh)
	go streamPipe(ctx, &wg, stderrPipe, stderrCh)

	go func() {
		wg.Wait()
		err := command.Wait()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		doneCh <- &CommandResult{
			ExitCode:   exitCode,
			Duration:   0,
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
		}
		close(doneCh)
	}()

	return &CommandStream{
		Stdout: stdoutCh,
		Stderr: stderrCh,
		Done:   doneCh,
		Cancel: cancel,
	}, nil
}

func (l *LoopbackClient) CopyTo(ctx context.Context, reader io.Reader, dst string) error {
	return ErrUnavailable
}

func (l *LoopbackClient) CopyFrom(ctx context.Context, src string, writer io.Writer) error {
	return ErrUnavailable
}

func (l *LoopbackClient) Close() error { return nil }

func streamPipe(ctx context.Context, wg *sync.WaitGroup, pipe io.Reader, out chan<- []byte) {
	defer wg.Done()
	reader := bufio.NewReader(pipe)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			chunk, err := reader.ReadBytes('\n')
			if len(chunk) > 0 {
				out <- chunk
			}
			if err != nil {
				close(out)
				return
			}
		}
	}
}

// validateWorkingDir checks if command arguments contain paths that would escape
// the working directory boundary. This provides basic protection in dev mode.
func validateWorkingDir(cmd *CommandRequest) error {
	if cmd.WorkingDir == "" {
		return nil
	}

	// Ensure working directory is absolute
	if !filepath.IsAbs(cmd.WorkingDir) {
		return fmt.Errorf("working directory must be absolute, got: %s", cmd.WorkingDir)
	}

	// Clean the working directory path
	workDir := filepath.Clean(cmd.WorkingDir)

	// Check all arguments for suspicious path patterns
	for _, arg := range cmd.Args {
		// Check if argument looks like a file path
		if strings.Contains(arg, "/") || strings.Contains(arg, "\\") {
			// Try to resolve relative paths against working directory
			var absPath string
			if filepath.IsAbs(arg) {
				absPath = filepath.Clean(arg)
			} else {
				absPath = filepath.Clean(filepath.Join(workDir, arg))
			}

			// Check if resolved path is outside working directory
			relPath, err := filepath.Rel(workDir, absPath)
			if err != nil || strings.HasPrefix(relPath, "..") {
				return fmt.Errorf("path %q escapes working directory %q (resolves to %q)", arg, workDir, absPath)
			}
		}
	}

	return nil
}
