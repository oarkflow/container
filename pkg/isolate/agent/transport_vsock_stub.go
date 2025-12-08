//go:build !linux

package agent

import (
	"context"
	"fmt"
	"net"
)

// VsockDialer is unavailable on non-Linux platforms.
type VsockDialer struct {
	CID  uint32
	Port uint32
}

func (d *VsockDialer) Dial(ctx context.Context) (net.Conn, error) {
	return nil, fmt.Errorf("vsock transport not supported on this platform")
}

// ListenVsock is unavailable on non-Linux platforms.
func ListenVsock(port uint32) (net.Listener, error) {
	return nil, fmt.Errorf("vsock transport not supported on this platform")
}
