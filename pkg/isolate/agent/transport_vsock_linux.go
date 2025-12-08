//go:build linux

package agent

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/mdlayher/vsock"
)

// VsockDialer dials an AF_VSOCK endpoint exposed by the guest.
type VsockDialer struct {
	CID     uint32
	Port    uint32
	Timeout time.Duration
}

func (d *VsockDialer) Dial(ctx context.Context) (net.Conn, error) {
	if d == nil {
		return nil, fmt.Errorf("vsock dialer not configured")
	}
	if d.Port == 0 {
		return nil, fmt.Errorf("vsock port is required")
	}
	if d.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	resultCh := make(chan struct {
		conn net.Conn
		err  error
	}, 1)

	go func() {
		conn, err := vsock.Dial(d.CID, d.Port, nil)
		var netConn net.Conn
		if err == nil {
			netConn = conn
		}
		resultCh <- struct {
			conn net.Conn
			err  error
		}{conn: netConn, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultCh:
		return res.conn, res.err
	}
}

// ListenVsock exposes a helper for agentd to bind a vsock port.
func ListenVsock(port uint32) (net.Listener, error) {
	if port == 0 {
		return nil, fmt.Errorf("vsock port is required")
	}
	return vsock.Listen(port, nil)
}
