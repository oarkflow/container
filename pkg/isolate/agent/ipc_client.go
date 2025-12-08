package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	defaultChunkSize  = 32 * 1024
	maxResultBytes    = 4 * 1024 * 1024
	execErrorExitCode = -1
	defaultFileMode   = 0o644
)

// Dialer dials a transport connection to the guest agent.
type Dialer interface {
	Dial(ctx context.Context) (net.Conn, error)
}

// IPCClient implements the Client interface over a framed IPC transport.
type IPCClient struct {
	dialer    Dialer
	chunkSize int
}

// NewIPCClient builds a transport-backed client instance.
func NewIPCClient(d Dialer) Client {
	return &IPCClient{
		dialer:    d,
		chunkSize: defaultChunkSize,
	}
}

func (c *IPCClient) Ping(ctx context.Context) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	writer := newFrameWriter(conn)
	dec := json.NewDecoder(conn)

	if err := writer.send(frameTypePing, nil); err != nil {
		return err
	}

	frame, err := readFrame(dec)
	if err != nil {
		return err
	}
	if frame.Type != frameTypePong {
		return fmt.Errorf("unexpected frame %s", frame.Type)
	}
	return nil
}

func (c *IPCClient) Exec(ctx context.Context, cmd *CommandRequest) (*CommandResult, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	writer := newFrameWriter(conn)
	dec := json.NewDecoder(conn)

	closeOnContext(ctx, conn)

	if err := c.sendExecRequest(ctx, writer, cmd, false); err != nil {
		return nil, err
	}

	return c.readExecResult(ctx, dec)
}

func (c *IPCClient) ExecStream(ctx context.Context, cmd *CommandRequest) (*CommandStream, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}

	streamCtx, cancel := context.WithCancel(ctx)

	writer := newFrameWriter(conn)
	dec := json.NewDecoder(conn)

	closeOnContext(streamCtx, conn)

	if err := c.sendExecRequest(streamCtx, writer, cmd, true); err != nil {
		cancel()
		conn.Close()
		return nil, err
	}

	stdoutCh := make(chan []byte, 32)
	stderrCh := make(chan []byte, 32)
	doneCh := make(chan *CommandResult, 1)

	go c.forwardStream(streamCtx, dec, stdoutCh, stderrCh, doneCh)

	return &CommandStream{
		Stdout: stdoutCh,
		Stderr: stderrCh,
		Done:   doneCh,
		Cancel: func() {
			cancel()
			conn.Close()
		},
	}, nil
}

func (c *IPCClient) CopyTo(ctx context.Context, reader io.Reader, dst string) error {
	if reader == nil {
		return fmt.Errorf("reader is required")
	}
	if dst == "" {
		return fmt.Errorf("destination path is required")
	}

	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	writer := newFrameWriter(conn)
	dec := json.NewDecoder(conn)
	closeOnContext(ctx, conn)

	if err := writer.send(frameTypeFilePutRequest, filePutRequestPayload{Path: dst, Mode: defaultFileMode}); err != nil {
		return err
	}

	buf := make([]byte, c.chunkSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if err := writer.send(frameTypeFilePutChunk, chunkPayload{Data: chunk}); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	if err := writer.send(frameTypeFilePutClose, nil); err != nil {
		return err
	}

	result, err := c.readFileTransferResult(ctx, dec, frameTypeFilePutResult)
	if err != nil {
		return err
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return nil
}

func (c *IPCClient) CopyFrom(ctx context.Context, src string, writer io.Writer) error {
	if writer == nil {
		return fmt.Errorf("writer is required")
	}
	if src == "" {
		return fmt.Errorf("source path is required")
	}

	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	frameWriter := newFrameWriter(conn)
	dec := json.NewDecoder(conn)
	closeOnContext(ctx, conn)

	if err := frameWriter.send(frameTypeFileGetRequest, fileGetRequestPayload{Path: src}); err != nil {
		return err
	}

	for {
		frame, err := readFrame(dec)
		if err != nil {
			return err
		}

		switch frame.Type {
		case frameTypeFileGetChunk:
			var payload chunkPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				return err
			}
			if len(payload.Data) > 0 {
				if _, err := writer.Write(payload.Data); err != nil {
					return err
				}
			}
		case frameTypeFileGetResult:
			var payload fileTransferResultPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				return err
			}
			if payload.Error != "" {
				return errors.New(payload.Error)
			}
			return nil
		case frameTypeError:
			var payload errorPayload
			_ = json.Unmarshal(frame.Payload, &payload)
			if payload.Message == "" {
				payload.Message = "file transfer error"
			}
			return errors.New(payload.Message)
		default:
			return fmt.Errorf("unexpected frame %s", frame.Type)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (c *IPCClient) Close() error { return nil }

func (c *IPCClient) readExecResult(ctx context.Context, dec *json.Decoder) (*CommandResult, error) {
	stdoutBuf := newLimitedBuffer(maxResultBytes)
	stderrBuf := newLimitedBuffer(maxResultBytes)

	for {
		frame, err := readFrame(dec)
		if err != nil {
			return nil, err
		}

		switch frame.Type {
		case frameTypeStdout:
			var payload chunkPayload
			if err := json.Unmarshal(frame.Payload, &payload); err == nil {
				stdoutBuf.Write(payload.Data)
			}
		case frameTypeStderr:
			var payload chunkPayload
			if err := json.Unmarshal(frame.Payload, &payload); err == nil {
				stderrBuf.Write(payload.Data)
			}
		case frameTypeResult:
			var payload execResultPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				return nil, err
			}
			if len(payload.Stdout) == 0 {
				payload.Stdout = stdoutBuf.Bytes()
			}
			if len(payload.Stderr) == 0 {
				payload.Stderr = stderrBuf.Bytes()
			}
			return payload.toCommandResult(), nil
		case frameTypeError:
			var payload errorPayload
			_ = json.Unmarshal(frame.Payload, &payload)
			return &CommandResult{ExitCode: execErrorExitCode, Stderr: []byte(payload.Message)}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

func (c *IPCClient) forwardStream(ctx context.Context, dec *json.Decoder, stdoutCh, stderrCh chan<- []byte, doneCh chan<- *CommandResult) {
	defer close(stdoutCh)
	defer close(stderrCh)
	defer close(doneCh)

	for {
		frame, err := readFrame(dec)
		if err != nil {
			doneCh <- &CommandResult{ExitCode: execErrorExitCode, Stderr: []byte(err.Error())}
			return
		}

		switch frame.Type {
		case frameTypeStdout:
			var payload chunkPayload
			if err := json.Unmarshal(frame.Payload, &payload); err == nil {
				select {
				case stdoutCh <- payload.Data:
				case <-ctx.Done():
					return
				}
			}
		case frameTypeStderr:
			var payload chunkPayload
			if err := json.Unmarshal(frame.Payload, &payload); err == nil {
				select {
				case stderrCh <- payload.Data:
				case <-ctx.Done():
					return
				}
			}
		case frameTypeResult:
			var payload execResultPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				doneCh <- &CommandResult{ExitCode: execErrorExitCode, Stderr: []byte(err.Error())}
			} else {
				doneCh <- payload.toCommandResult()
			}
			return
		case frameTypeError:
			var payload errorPayload
			_ = json.Unmarshal(frame.Payload, &payload)
			doneCh <- &CommandResult{ExitCode: execErrorExitCode, Stderr: []byte(payload.Message)}
			return
		}

		select {
		case <-ctx.Done():
			doneCh <- &CommandResult{ExitCode: execErrorExitCode, Stderr: []byte(ctx.Err().Error())}
			return
		default:
		}
	}
}

func (c *IPCClient) sendExecRequest(ctx context.Context, writer *frameWriter, cmd *CommandRequest, stream bool) error {
	req := execRequestPayload{
		Path:       cmd.Path,
		Args:       append([]string(nil), cmd.Args...),
		Env:        cmd.Env,
		WorkingDir: cmd.WorkingDir,
		Stream:     stream,
		User:       cmd.User,
	}
	if cmd.Timeout > 0 {
		req.TimeoutMilli = cmd.Timeout.Milliseconds()
	}

	if err := writer.send(frameTypeExecRequest, req); err != nil {
		return err
	}

	go c.pipeStdin(ctx, writer, cmd.Stdin)
	return nil
}

func (c *IPCClient) pipeStdin(ctx context.Context, writer *frameWriter, reader io.Reader) {
	if reader == nil {
		_ = writer.send(frameTypeStdinClose, nil)
		return
	}

	buf := make([]byte, c.chunkSize)
	for {
		select {
		case <-ctx.Done():
			_ = writer.send(frameTypeStdinClose, nil)
			return
		default:
		}

		n, err := reader.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if sendErr := writer.send(frameTypeStdinChunk, stdinPayload{Data: chunk}); sendErr != nil {
				return
			}
		}
		if errors.Is(err, io.EOF) {
			_ = writer.send(frameTypeStdinClose, nil)
			return
		}
		if err != nil {
			_ = writer.send(frameTypeStdinClose, nil)
			return
		}
	}
}

func (c *IPCClient) dial(ctx context.Context) (net.Conn, error) {
	return c.dialer.Dial(ctx)
}

func closeOnContext(ctx context.Context, conn net.Conn) {
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
}

func (c *IPCClient) readFileTransferResult(ctx context.Context, dec *json.Decoder, resultType frameType) (*fileTransferResultPayload, error) {
	for {
		frame, err := readFrame(dec)
		if err != nil {
			return nil, err
		}
		switch frame.Type {
		case resultType:
			var payload fileTransferResultPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				return nil, err
			}
			return &payload, nil
		case frameTypeError:
			var payload errorPayload
			_ = json.Unmarshal(frame.Payload, &payload)
			if payload.Message == "" {
				payload.Message = "file transfer error"
			}
			return nil, errors.New(payload.Message)
		default:
			return nil, fmt.Errorf("unexpected frame %s", frame.Type)
		}
	}
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return 0, nil
	}

	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}

	if len(p) > remaining {
		p = p[:remaining]
	}

	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (p execResultPayload) toCommandResult() *CommandResult {
	return &CommandResult{
		ExitCode:   p.ExitCode,
		Stdout:     append([]byte(nil), p.Stdout...),
		Stderr:     append([]byte(nil), p.Stderr...),
		Duration:   time.Duration(p.DurationMilli) * time.Millisecond,
		StartedAt:  p.StartedAt,
		FinishedAt: p.FinishedAt,
	}
}
