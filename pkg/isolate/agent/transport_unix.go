//go:build !windows

package agent

import (
	"context"
	"fmt"
	"net"
	"time"
)

// UnixDialer connects to a guest agent exposed via a Unix domain socket.
type UnixDialer struct {
	Path    string
	Timeout time.Duration
}

func (d *UnixDialer) Dial(ctx context.Context) (net.Conn, error) {
	if d == nil || d.Path == "" {
		return nil, fmt.Errorf("unix path is required")
	}
	var nd net.Dialer
	if d.Timeout > 0 {
		nd.Timeout = d.Timeout
	}
	return nd.DialContext(ctx, "unix", d.Path)
}
