package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ServerConfig tunes the IPC agent server behavior.
type ServerConfig struct {
	ChunkSize       int
	MaxResultBuffer int
	Logger          *log.Logger
}

// Server executes guest commands upon requests from the host.
type Server struct {
	chunkSize int
	bufLimit  int
	logger    *log.Logger
}

// NewServer constructs a new agent server with sane defaults.
func NewServer(cfg ServerConfig) *Server {
	chunk := cfg.ChunkSize
	if chunk <= 0 {
		chunk = defaultChunkSize
	}
	limit := cfg.MaxResultBuffer
	if limit <= 0 {
		limit = maxResultBytes
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Server{chunkSize: chunk, bufLimit: limit, logger: logger}
}

// Serve accepts incoming connections and handles them concurrently.
func (s *Server) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				s.logger.Printf("accept error: %v", err)
				continue
			}
			return err
		}
		go s.handleConn(conn)
	}
}

// ServeConn handles a single IPC connection.
func (s *Server) ServeConn(conn net.Conn) {
	s.handleConn(conn)
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(bufio.NewReader(conn))
	writer := newFrameWriter(conn)

	for {
		frame, err := readFrame(dec)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.Printf("frame error: %v", err)
			}
			return
		}

		switch frame.Type {
		case frameTypePing:
			_ = writer.send(frameTypePong, pongPayload{Timestamp: time.Now()})
		case frameTypeExecRequest:
			var payload execRequestPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
				return
			}
			s.runExec(conn, dec, writer, payload)
			return
		case frameTypeFilePutRequest:
			var payload filePutRequestPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
				return
			}
			s.handleFilePut(dec, writer, payload)
			return
		case frameTypeFileGetRequest:
			var payload fileGetRequestPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
				return
			}
			s.handleFileGet(writer, payload)
			return
		default:
			_ = writer.send(frameTypeError, errorPayload{Message: "unsupported frame"})
			return
		}
	}
}

func (s *Server) runExec(conn net.Conn, dec *json.Decoder, writer *frameWriter, payload execRequestPayload) {
	execCtx := context.Background()
	if payload.TimeoutMilli > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(execCtx, time.Duration(payload.TimeoutMilli)*time.Millisecond)
		defer cancel()
	}

	command := exec.CommandContext(execCtx, payload.Path, payload.Args...)
	command.Dir = payload.WorkingDir
	command.Env = flattenEnv(nil, payload.Env)

	stdinPipe, err := command.StdinPipe()
	if err != nil {
		_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
		return
	}
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
		return
	}
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
		return
	}

	if err := command.Start(); err != nil {
		_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
		return
	}

	startTime := time.Now()

	stdoutBuf := newLimitedBuffer(s.bufLimit)
	stderrBuf := newLimitedBuffer(s.bufLimit)

	wg := sync.WaitGroup{}
	wg.Add(2)

	go s.streamPipe(stdoutPipe, stdoutBuf, writer, payload.Stream, frameTypeStdout, &wg)
	go s.streamPipe(stderrPipe, stderrBuf, writer, payload.Stream, frameTypeStderr, &wg)

	stdinDone := make(chan struct{})
	go s.consumeStdin(dec, writer, stdinPipe, stdinDone)

	err = command.Wait()

	_ = conn.SetReadDeadline(time.Now())
	<-stdinDone
	wg.Wait()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
			return
		}
	}

	result := execResultPayload{
		ExitCode:      exitCode,
		Stdout:        stdoutBuf.Bytes(),
		Stderr:        stderrBuf.Bytes(),
		DurationMilli: time.Since(startTime).Milliseconds(),
		StartedAt:     startTime,
		FinishedAt:    time.Now(),
	}
	_ = writer.send(frameTypeResult, result)
}

func (s *Server) streamPipe(reader io.Reader, collector *limitedBuffer, writer *frameWriter, stream bool, typ frameType, wg *sync.WaitGroup) {
	defer wg.Done()
	buf := make([]byte, s.chunkSize)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			collector.Write(chunk)
			if stream {
				_ = writer.send(typ, chunkPayload{Data: chunk})
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) consumeStdin(dec *json.Decoder, writer *frameWriter, stdin io.WriteCloser, done chan<- struct{}) {
	defer func() {
		stdin.Close()
		close(done)
	}()

	for {
		frame, err := readFrame(dec)
		if err != nil {
			return
		}
		switch frame.Type {
		case frameTypeStdinChunk:
			var payload stdinPayload
			if err := json.Unmarshal(frame.Payload, &payload); err == nil {
				_, _ = stdin.Write(payload.Data)
			}
		case frameTypeStdinClose:
			return
		case frameTypePing:
			_ = writer.send(frameTypePong, pongPayload{Timestamp: time.Now()})
		default:
			return
		}
	}
}

func (s *Server) handleFilePut(dec *json.Decoder, writer *frameWriter, payload filePutRequestPayload) {
	if payload.Path == "" {
		_ = writer.send(frameTypeError, errorPayload{Message: "path is required"})
		return
	}
	mode := os.FileMode(payload.Mode)
	if mode == 0 {
		mode = defaultFileMode
	}
	if dir := filepath.Dir(payload.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
			return
		}
	}
	file, err := os.OpenFile(payload.Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
		return
	}
	defer file.Close()

	var written int64
	for {
		frame, err := readFrame(dec)
		if err != nil {
			_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
			return
		}
		switch frame.Type {
		case frameTypeFilePutChunk:
			var chunk chunkPayload
			if err := json.Unmarshal(frame.Payload, &chunk); err != nil {
				_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
				return
			}
			if len(chunk.Data) > 0 {
				n, err := file.Write(chunk.Data)
				if err != nil {
					_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
					return
				}
				written += int64(n)
			}
		case frameTypeFilePutClose:
			_ = writer.send(frameTypeFilePutResult, fileTransferResultPayload{Bytes: written})
			return
		default:
			_ = writer.send(frameTypeError, errorPayload{Message: "unexpected frame during file upload"})
			return
		}
	}
}

func (s *Server) handleFileGet(writer *frameWriter, payload fileGetRequestPayload) {
	if payload.Path == "" {
		_ = writer.send(frameTypeError, errorPayload{Message: "path is required"})
		return
	}
	file, err := os.Open(payload.Path)
	if err != nil {
		_ = writer.send(frameTypeError, errorPayload{Message: err.Error()})
		return
	}
	defer file.Close()

	buf := make([]byte, s.chunkSize)
	var sent int64
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			sent += int64(len(chunk))
			if err := writer.send(frameTypeFileGetChunk, chunkPayload{Data: chunk}); err != nil {
				return
			}
		}
		if errors.Is(readErr, io.EOF) {
			_ = writer.send(frameTypeFileGetResult, fileTransferResultPayload{Bytes: sent})
			return
		}
		if readErr != nil {
			_ = writer.send(frameTypeFileGetResult, fileTransferResultPayload{Bytes: sent, Error: readErr.Error()})
			return
		}
	}
}
