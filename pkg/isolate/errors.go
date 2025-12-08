package isolate

import "errors"

var (
	ErrContainerExists      = errors.New("container already exists")
	ErrContainerNotFound    = errors.New("container not found")
	ErrContainerNotCreated  = errors.New("container not created")
	ErrRuntimeUnavailable   = errors.New("no runtime available for this host")
	ErrExecutionUnavailable = errors.New("guest agent unavailable for execution")
)
