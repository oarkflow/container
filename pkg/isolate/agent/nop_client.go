package agent

import (
	"context"
	"io"
)

// NopClient satisfies the Client interface while returning ErrUnavailable for
// every operation. This lets runtimes compile without a real guest agent until
// integration is complete.
type NopClient struct{}

// NewNopClient constructs a no-op client instance.
func NewNopClient() Client {
	return &NopClient{}
}

func (n *NopClient) Ping(ctx context.Context) error {
	return ErrUnavailable
}

func (n *NopClient) Exec(ctx context.Context, cmd *CommandRequest) (*CommandResult, error) {
	return nil, ErrUnavailable
}

func (n *NopClient) ExecStream(ctx context.Context, cmd *CommandRequest) (*CommandStream, error) {
	return nil, ErrUnavailable
}

func (n *NopClient) CopyTo(ctx context.Context, reader io.Reader, dst string) error {
	return ErrUnavailable
}

func (n *NopClient) CopyFrom(ctx context.Context, src string, writer io.Writer) error {
	return ErrUnavailable
}

func (n *NopClient) Close() error { return nil }
