package agent

import (
	"context"
	"io"
	"time"
)

// CommandRequest is the wire format for guest execution requests.
type CommandRequest struct {
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

// CommandResult captures stdout/stderr snapshots and the exit code.
type CommandResult struct {
	ExitCode   int
	Stdout     []byte
	Stderr     []byte
	Duration   time.Duration
	StartedAt  time.Time
	FinishedAt time.Time
}

// CommandStream supports real-time IO streaming.
type CommandStream struct {
	Stdout <-chan []byte
	Stderr <-chan []byte
	Done   <-chan *CommandResult
	Cancel context.CancelFunc
}

// Client is implemented by guest agents or proxies that can execute commands
// inside the running VM.
type Client interface {
	Ping(ctx context.Context) error
	Exec(ctx context.Context, cmd *CommandRequest) (*CommandResult, error)
	ExecStream(ctx context.Context, cmd *CommandRequest) (*CommandStream, error)
	CopyTo(ctx context.Context, reader io.Reader, dst string) error
	CopyFrom(ctx context.Context, src string, writer io.Writer) error
	Close() error
}
