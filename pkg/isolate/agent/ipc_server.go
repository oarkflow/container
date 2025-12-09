package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ServerConfig tunes the IPC agent server behavior.
type ServerConfig struct {
	ChunkSize       int
	MaxResultBuffer int
	Logger          *log.Logger
	RootDir         string // If set, restricts all operations to this directory
	UseChrootIfRoot bool   // If true and running as root, use chroot for isolation
	AllowInsecure   bool   // If true, allow interpreter execution without chroot (INSECURE - dev only)
}

// Server executes guest commands upon requests from the host.
type Server struct {
	chunkSize       int
	bufLimit        int
	logger          *log.Logger
	rootDir         string          // If set, restricts all operations to this directory
	chrootExecutor  *ChrootExecutor // Used for OS-level isolation when available
	useChrootIfRoot bool
	allowInsecure   bool // Allow interpreter execution without chroot (INSECURE)
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
	rootDir := ""
	var chrootExec *ChrootExecutor
	if cfg.RootDir != "" {
		var err error
		rootDir, err = filepath.Abs(cfg.RootDir)
		if err != nil {
			logger.Printf("warning: invalid root dir %q: %v", cfg.RootDir, err)
		} else {
			logger.Printf("restricting operations to: %s", rootDir)

			// Try to set up chroot if requested
			if cfg.UseChrootIfRoot {
				chrootExec, err = NewChrootExecutor(rootDir)
				if err != nil {
					logger.Printf("ERROR: chroot setup failed: %v", err)
					logger.Printf("ERROR: Cannot provide secure isolation - aborting")
					logger.Printf("HINT: Run with 'sudo' or use --no-chroot flag (insecure for untrusted code)")
					panic(fmt.Sprintf("chroot required but failed: %v", err))
				} else if chrootExec.RequiresRoot() {
					logger.Printf("ERROR: chroot requires root privileges")
					logger.Printf("ERROR: Cannot provide secure isolation - aborting")
					logger.Printf("HINT: Run with 'sudo' or use --no-chroot flag (insecure for untrusted code)")
					panic("chroot required but not running as root")
				} else {
					logger.Printf("✓ chroot isolation enabled - secure execution mode")
				}
			}
		}
	}
	return &Server{
		chunkSize:       chunk,
		bufLimit:        limit,
		logger:          logger,
		rootDir:         rootDir,
		chrootExecutor:  chrootExec,
		useChrootIfRoot: cfg.UseChrootIfRoot,
		allowInsecure:   cfg.AllowInsecure,
	}
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
	// Validate paths if rootDir is set (note: this only validates arguments, not script contents)
	if s.rootDir != "" {
		if s.chrootExecutor == nil {
			// Without chroot, we only have weak path validation
			if err := s.validatePaths(&payload); err != nil {
				_ = writer.send(frameTypeError, errorPayload{Message: "security violation: " + err.Error()})
				return
			}

			// Block interpreters without chroot unless explicitly allowed
			if s.isInterpreter(payload.Path) && !s.allowInsecure {
				s.logger.Printf("ERROR: refusing to execute interpreter %q without chroot isolation", payload.Path)
				_ = writer.send(frameTypeError, errorPayload{
					Message: fmt.Sprintf("security error: cannot execute interpreter %q without chroot isolation - scripts can escape root directory. Start agent with 'sudo' for secure mode", payload.Path),
				})
				return
			} else if s.isInterpreter(payload.Path) && s.allowInsecure {
				s.logger.Printf("WARNING: executing interpreter %q in INSECURE mode - scripts can escape root directory!", payload.Path)
			}
		}
	}

	execCtx := context.Background()
	if payload.TimeoutMilli > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(execCtx, time.Duration(payload.TimeoutMilli)*time.Millisecond)
		defer cancel()
	}

	command := exec.CommandContext(execCtx, payload.Path, payload.Args...)
	command.Dir = payload.WorkingDir
	command.Env = flattenEnv(nil, payload.Env)

	// Apply chroot isolation if available
	if s.chrootExecutor != nil {
		if err := s.chrootExecutor.PrepareCommand(command, payload.WorkingDir); err != nil {
			_ = writer.send(frameTypeError, errorPayload{Message: "chroot setup failed: " + err.Error()})
			return
		}
	}

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

// validatePaths ensures that all paths in the exec request are within the rootDir boundary.
func (s *Server) validatePaths(payload *execRequestPayload) error {
	if s.rootDir == "" {
		return nil
	}

	// Reject shell interpreters when rootDir is set UNLESS they're executing a script file within root
	shellCommands := []string{"/bin/sh", "/bin/bash", "/bin/zsh", "sh", "bash", "zsh",
		"cmd.exe", "powershell.exe", "pwsh.exe", "cmd", "powershell", "pwsh"}
	isShell := false
	for _, shell := range shellCommands {
		if strings.HasSuffix(payload.Path, shell) || payload.Path == shell {
			isShell = true
			break
		}
	}

	if isShell {
		// Check if shell is executing a script file (not using -c flag)
		hasScriptArg := false
		for _, arg := range payload.Args {
			// If it's -c flag, reject it (inline commands can bypass restrictions)
			if arg == "-c" {
				return fmt.Errorf("shell commands with -c flag are not allowed when root directory isolation is enabled")
			}
			// Check if there's a script file argument
			if !strings.HasPrefix(arg, "-") && (strings.HasSuffix(arg, ".sh") || strings.HasSuffix(arg, ".bash") ||
				strings.HasSuffix(arg, ".ps1") || strings.HasSuffix(arg, ".bat") || strings.HasSuffix(arg, ".cmd")) {
				hasScriptArg = true
			}
		}

		// If no script file found, reject the shell command
		if !hasScriptArg {
			return fmt.Errorf("shell commands without script files are not allowed when root directory isolation is enabled")
		}
	}

	// Validate working directory
	if payload.WorkingDir != "" {
		if err := s.checkPathWithinRoot(payload.WorkingDir, "working directory"); err != nil {
			return err
		}
	} else {
		// If no working directory specified, set it to rootDir
		payload.WorkingDir = s.rootDir
	}

	// Validate the command path itself if it's a relative path
	if !filepath.IsAbs(payload.Path) && (strings.Contains(payload.Path, "/") || strings.Contains(payload.Path, "\\")) {
		absPath := filepath.Clean(filepath.Join(payload.WorkingDir, payload.Path))
		relPath, err := filepath.Rel(s.rootDir, absPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("command path %q escapes root %q", payload.Path, s.rootDir)
		}
	}

	// Check all arguments for file paths that might escape
	for i, arg := range payload.Args {
		// Check if argument looks like a file path (but not command flags like -c, --flag, etc.)
		if (strings.Contains(arg, "/") || strings.Contains(arg, "\\")) && !strings.HasPrefix(arg, "-") {
			// Resolve path relative to working directory
			var absPath string
			if filepath.IsAbs(arg) {
				absPath = filepath.Clean(arg)
			} else {
				absPath = filepath.Clean(filepath.Join(payload.WorkingDir, arg))
			}

			// Check if resolved path is within rootDir
			relPath, err := filepath.Rel(s.rootDir, absPath)
			if err != nil || strings.HasPrefix(relPath, "..") {
				return fmt.Errorf("argument %d path %q escapes root %q (resolves to %q)", i, arg, s.rootDir, absPath)
			}
		}
	}

	return nil
}

// isInterpreter checks if the command is a script interpreter
func (s *Server) isInterpreter(cmdPath string) bool {
	interpreters := []string{
		"python", "python2", "python3",
		"node", "nodejs",
		"ruby", "irb",
		"php",
		"perl",
		"lua",
		"java", "javac",
		"go", "gofmt",
		"bash", "sh", "zsh", "fish", "ksh",
		"cmd.exe", "powershell.exe", "pwsh.exe",
	}

	baseName := filepath.Base(cmdPath)
	for _, interp := range interpreters {
		if baseName == interp || strings.HasPrefix(baseName, interp) {
			return true
		}
	}
	return false
}

// checkPathWithinRoot verifies a single path is within the root directory.
func (s *Server) checkPathWithinRoot(path, pathType string) error {
	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath = filepath.Clean(filepath.Join(s.rootDir, path))
	}

	relPath, err := filepath.Rel(s.rootDir, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("%s %q is outside root %q", pathType, path, s.rootDir)
	}

	return nil
}
