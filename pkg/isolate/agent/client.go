package agent

import "errors"

var (
	ErrUnavailable = errors.New("guest agent unavailable")
)
