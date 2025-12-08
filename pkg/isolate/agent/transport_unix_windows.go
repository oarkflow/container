//go:build windows

package agent

import (
	"context"
	"fmt"
	"net"
)

// UnixDialer is not supported on Windows hosts.
type UnixDialer struct {
	Path string
}

func (d *UnixDialer) Dial(ctx context.Context) (net.Conn, error) {
	return nil, fmt.Errorf("unix domain sockets not supported on Windows")
}
