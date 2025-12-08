package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/oarkflow/container/pkg/isolate/agent"
)

func main() {
	unixPath := flag.String("unix", "", "Unix domain socket path to listen on")
	vsockPort := flag.Uint("vsock-port", 0, "AF_VSOCK port to listen on (Linux guests)")
	chunkSize := flag.Int("chunk", 32*1024, "Chunk size for stdout/stderr streaming")
	maxBuffer := flag.Int("max-buffer", 4*1024*1024, "Maximum bytes to retain per stream in the final result")
	flag.Parse()

	if *unixPath == "" && *vsockPort == 0 {
		fmt.Fprintln(os.Stderr, "agentd requires -unix or -vsock-port")
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "[agentd] ", log.LstdFlags)
	srv := agent.NewServer(agent.ServerConfig{
		ChunkSize:       *chunkSize,
		MaxResultBuffer: *maxBuffer,
		Logger:          logger,
	})

	listeners := make([]net.Listener, 0, 2)

	if *unixPath != "" {
		_ = os.Remove(*unixPath)
		ln, err := net.Listen("unix", *unixPath)
		if err != nil {
			logger.Fatalf("listen unix: %v", err)
		}
		listeners = append(listeners, ln)
		logger.Printf("listening on unix socket %s", *unixPath)
		go func() {
			if err := srv.Serve(ln); err != nil {
				logger.Printf("unix listener error: %v", err)
			}
		}()
	}

	if *vsockPort != 0 {
		ln, err := agent.ListenVsock(uint32(*vsockPort))
		if err != nil {
			logger.Fatalf("listen vsock: %v", err)
		}
		listeners = append(listeners, ln)
		logger.Printf("listening on vsock port %d", *vsockPort)
		go func() {
			if err := srv.Serve(ln); err != nil {
				logger.Printf("vsock listener error: %v", err)
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Println("shutting down")

	for _, ln := range listeners {
		_ = ln.Close()
	}

	if *unixPath != "" {
		_ = os.Remove(*unixPath)
	}
}
