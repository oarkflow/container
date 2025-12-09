package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/oarkflow/container/pkg/isolate"
	runtimectl "github.com/oarkflow/container/pkg/isolate/runtime"
)

func getDefaultSocketPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "./agent.sock"
	}
	return filepath.Join(homeDir, ".container", "agent.sock")
}

func getDefaultRootDir() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx := context.Background()

	listRuntimes := flag.Bool("list", false, "List available runtimes and exit")
	image := flag.String("image", "", "Path or name of the VM image to use")
	memory := flag.Int64("memory", 512*1024*1024, "Memory in bytes")
	cpus := flag.Int("cpus", 2, "Number of vCPUs")
	devMode := flag.Bool("dev", false, "Use loopback agent for local testing (executes on host)")
	agentUnix := flag.String("agent-unix", "", "Path to a Unix socket (default: ~/.container/agent.sock)")
	autoAgent := flag.Bool("auto-agent", true, "Automatically start/manage agent daemon")
	noAgent := flag.Bool("no-agent", false, "Disable agent mode and use full VM (requires --image)")
	agentVsockCID := flag.Uint("agent-vsock-cid", 3, "vsock CID for the guest (Linux only)")
	agentVsockPort := flag.Uint("agent-vsock-port", 0, "vsock port for the guest agent (requires CID)")
	rootDir := flag.String("root", "", "Root directory for agent isolation (default: current directory)")
	workdir := flag.String("workdir", "/workspace", "Guest working directory (used with --root)")
	cmdFlag := flag.String("cmd", "", "Command to execute as a shell command (not recommended with isolated agent)")
	flag.Parse()

	if *listRuntimes {
		describeRuntimes()
		return 0
	}

	if *cmdFlag == "" && flag.NArg() == 0 {
		fmt.Println("usage: isolatectl [flags] <command> [args...]")
		fmt.Println("\nExamples:")
		fmt.Println("  isolatectl cat file.txt              # Uses default agent at ~/.container/agent.sock")
		fmt.Println("  isolatectl ls -la                    # Auto-starts agent if needed")
		fmt.Println("  isolatectl --root=/data cat file.txt # Restricts operations to /data")
		flag.PrintDefaults()
		return 1
	}

	// Set default socket path if not provided (unless explicitly disabled)
	if *agentUnix == "" && !*noAgent {
		*agentUnix = getDefaultSocketPath()
	}

	// Determine if we're using direct agent mode (agent-unix without VM)
	usingDirectAgent := *agentUnix != "" && !*devMode && !*noAgent

	// Set default root directory for agent isolation
	agentRootDir := ""
	if usingDirectAgent {
		if *rootDir != "" {
			agentRootDir = *rootDir
		} else {
			agentRootDir = getDefaultRootDir()
		}
	}

	// Start agent manager if auto-agent is enabled
	var agentMgr *isolate.AgentManager
	if *autoAgent && usingDirectAgent && *agentVsockPort == 0 {
		agentMgr = isolate.NewAgentManager(*agentUnix, agentRootDir)
		if err := agentMgr.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to start agent: %v\n", err)
			fmt.Fprintln(os.Stderr, "continuing without auto-managed agent...")
		} else {
			defer agentMgr.Stop()
			fmt.Fprintf(os.Stderr, "[agent] started at %s (root: %s)\n", *agentUnix, agentRootDir)
		}
	}

	// If using direct agent mode, execute directly without creating a VM
	if usingDirectAgent {
		return runDirectAgent(ctx, *agentUnix, agentRootDir, *cmdFlag, flag.Args())
	}

	manager, err := isolate.NewDefaultManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize runtime: %v\n", err)
		return 1
	}

	name := fmt.Sprintf("job-%d", time.Now().UnixNano())
	metadata := map[string]string{}
	if *agentUnix != "" {
		metadata["agent.unix"] = *agentUnix
	}
	if *agentVsockPort != 0 {
		metadata["agent.vsock.cid"] = fmt.Sprintf("%d", *agentVsockCID)
		metadata["agent.vsock.port"] = fmt.Sprintf("%d", *agentVsockPort)
	}

	// Resolve root directory to absolute path if provided
	var absRootDir string
	if *rootDir != "" {
		var err error
		absRootDir, err = filepath.Abs(*rootDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve root directory: %v\n", err)
			return 1
		}
	}

	mounts := make([]runtimectl.Mount, 0, 1)
	if absRootDir != "" {
		// In dev mode, workdir must be absolute and point to the mounted location
		var targetPath string
		if *devMode {
			// For dev mode, use the absolute host path as the working directory
			targetPath = absRootDir
		} else {
			// For real VM mode, use the guest path
			targetPath = *workdir
		}

		mounts = append(mounts, runtimectl.Mount{
			Source:   absRootDir,
			Target:   targetPath,
			Type:     runtimectl.MountTypeBind,
			ReadOnly: false,
		})

		// Update workdir to use the target path
		*workdir = targetPath
	}

	cfg := &isolate.Config{
		Name:        name,
		Image:       *image,
		CPUs:        *cpus,
		Memory:      *memory,
		DiskSize:    4 * 1024 * 1024 * 1024,
		NetworkMode: runtimectl.NetworkModeNAT,
		Environment: map[string]string{},
		Metadata:    metadata,
		DevMode:     *devMode,
		Mounts:      mounts,
	}
	if *rootDir != "" {
		cfg.WorkingDir = *workdir
	}

	container, err := manager.CreateContainer(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create container: %v\n", err)
		return 1
	}
	defer manager.DeleteContainer(context.Background(), name)

	if *devMode {
		fmt.Fprintln(os.Stderr, "[warning] dev mode executes commands directly on this host. Use only for testing.")
		if absRootDir != "" {
			fmt.Fprintf(os.Stderr, "[warning] commands will be restricted to: %s\n", absRootDir)
		} else {
			fmt.Fprintln(os.Stderr, "[warning] no root directory specified - commands will run unrestricted!")
		}
	}

	if err := container.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start container: %v\n", err)
		return 1
	}

	cmdPath, cmdArgs := resolveCommand(*cmdFlag, flag.Args())
	if cmdPath == "" {
		fmt.Fprintln(os.Stderr, "no command provided")
		return 1
	}

	command := &isolate.Command{
		Path: cmdPath,
		Args: cmdArgs,
		Env:  map[string]string{},
	}
	if cfg.WorkingDir != "" {
		command.WorkingDir = cfg.WorkingDir
	}

	result, err := container.Exec(ctx, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "exec failed: %v\n", err)
		return 1
	}

	if len(result.Stdout) > 0 {
		if _, err := os.Stdout.Write(result.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "write stdout: %v\n", err)
		}
	}
	if len(result.Stderr) > 0 {
		if _, err := os.Stderr.Write(result.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "write stderr: %v\n", err)
		}
	}

	if status, err := container.Status(ctx); err == nil {
		printStatus(status)
	} else {
		fmt.Fprintf(os.Stderr, "failed to fetch status: %v\n", err)
	}

	if stats, err := container.Stats(ctx); err == nil {
		printStats(stats)
	} else {
		fmt.Fprintf(os.Stderr, "failed to fetch stats: %v\n", err)
	}

	return result.ExitCode
}

// runDirectAgent executes a command directly via the agent without creating a VM
func runDirectAgent(ctx context.Context, socketPath, rootDir, cmdFlag string, positionalArgs []string) int {
	// Connect to agent
	client := isolate.NewAgentClient(socketPath)

	// Resolve command
	cmdPath, cmdArgs := resolveCommand(cmdFlag, positionalArgs)
	if cmdPath == "" {
		fmt.Fprintln(os.Stderr, "no command provided")
		return 1
	}

	// Check if command is a shell and warn about script execution
	if isShellCommand(cmdPath) && len(cmdArgs) > 0 {
		// Check if script file exists and validate it's within root
		for _, arg := range cmdArgs {
			if !strings.HasPrefix(arg, "-") && (strings.HasSuffix(arg, ".sh") || strings.HasSuffix(arg, ".bash")) {
				fmt.Fprintf(os.Stderr, "[warning] executing script %q - script contents are NOT validated for path escaping\n", arg)
				break
			}
		}
	}

	// Create command request
	command := &isolate.Command{
		Path:       cmdPath,
		Args:       cmdArgs,
		Env:        map[string]string{},
		WorkingDir: rootDir,
	}

	// Execute command
	result, err := client.Exec(ctx, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "exec failed: %v\n", err)
		return 1
	}

	// Write output
	if len(result.Stdout) > 0 {
		if _, err := os.Stdout.Write(result.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "write stdout: %v\n", err)
		}
	}
	if len(result.Stderr) > 0 {
		if _, err := os.Stderr.Write(result.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "write stderr: %v\n", err)
		}
	}

	return result.ExitCode
}

func describeRuntimes() {
	targetOS := runtime.GOOS
	descriptors := runtimectl.AvailableRuntimes(targetOS)
	if len(descriptors) == 0 {
		fmt.Println("no runtimes registered for this platform")
		return
	}

	fmt.Printf("available runtimes for %s:\n", targetOS)
	for _, desc := range descriptors {
		fmt.Printf("- %s (hypervisor=%s priority=%d)\n", desc.Name, desc.Hypervisor, desc.Priority)
	}
}

func resolveCommand(cmdString string, positional []string) (string, []string) {
	if cmdString != "" {
		return shellCommandForHost(cmdString)
	}
	if len(positional) == 0 {
		return "", nil
	}
	path := positional[0]
	args := append([]string(nil), positional[1:]...)
	return path, args
}

func shellCommandForHost(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func isShellCommand(cmdPath string) bool {
	shells := []string{"sh", "bash", "zsh", "fish", "ksh", "cmd.exe", "powershell.exe", "pwsh.exe"}
	baseName := filepath.Base(cmdPath)
	for _, shell := range shells {
		if baseName == shell || strings.HasSuffix(baseName, "/"+shell) || strings.HasSuffix(baseName, "\\"+shell) {
			return true
		}
	}
	return false
}

func printStatus(status *isolate.Status) {
	if status == nil {
		return
	}

	fmt.Fprintln(os.Stderr, "\n[container status]")
	fmt.Fprintf(os.Stderr, "  name: %s\n", status.Name)
	fmt.Fprintf(os.Stderr, "  state: %s\n", status.State)
	fmt.Fprintf(os.Stderr, "  created: %s\n", formatRelative(status.CreatedAt))
	fmt.Fprintf(os.Stderr, "  started: %s\n", formatRelative(status.StartedAt))
	fmt.Fprintf(os.Stderr, "  updated: %s\n", formatRelative(status.UpdatedAt))
	if status.GuestIP != "" {
		fmt.Fprintf(os.Stderr, "  guest ip: %s\n", status.GuestIP)
	}
	if len(status.ResolvedIPs) > 0 {
		fmt.Fprintf(os.Stderr, "  resolved: %s\n", strings.Join(status.ResolvedIPs, ", "))
	}
	if len(status.NetworkPlan) > 0 {
		fmt.Fprintln(os.Stderr, "  network plan:")
		for _, step := range status.NetworkPlan {
			fmt.Fprintf(os.Stderr, "    - %s\n", step)
		}
	}
	if len(status.Interfaces) > 0 {
		fmt.Fprintln(os.Stderr, "  interfaces:")
		for _, iface := range status.Interfaces {
			fmt.Fprintf(os.Stderr, "    - %s (%s)\n", iface.Name, valueOrDefault(iface.HostDevice, "n/a"))
			fmt.Fprintf(os.Stderr, "      mac=%s state=%s bridge=%s switch=%s\n",
				valueOrDefault(iface.MACAddress, "n/a"), valueOrDefault(iface.State, "unknown"), valueOrDefault(iface.Bridge, "n/a"), valueOrDefault(iface.Switch, "n/a"))
			fmt.Fprintf(os.Stderr, "      ipv4=%s ipv6=%s\n", valueOrDefault(iface.GuestIPv4, "n/a"), valueOrDefault(iface.GuestIPv6, "n/a"))
			if len(iface.PortForwards) > 0 {
				fmt.Fprintln(os.Stderr, "      forwards:")
				for _, pf := range iface.PortForwards {
					fmt.Fprintf(os.Stderr, "        * %s\n", formatPortForward(pf))
				}
			}
			if len(iface.FirewallRules) > 0 {
				fmt.Fprintln(os.Stderr, "      firewall:")
				for _, rule := range iface.FirewallRules {
					fmt.Fprintf(os.Stderr, "        * %s\n", rule)
				}
			}
		}
	}
}

func printStats(stats *isolate.Stats) {
	if stats == nil {
		return
	}

	fmt.Fprintln(os.Stderr, "\n[resource metrics]")
	fmt.Fprintf(os.Stderr, "  cpu: %.1f%%\n", stats.CPUPercent)
	fmt.Fprintf(os.Stderr, "  memory: %s\n", formatBytes(stats.MemoryBytes))
	fmt.Fprintf(os.Stderr, "  disk: %s\n", formatBytes(stats.DiskBytes))
	fmt.Fprintf(os.Stderr, "  network rx: %s\n", formatBytes(stats.NetworkRxBytes))
	fmt.Fprintf(os.Stderr, "  network tx: %s\n", formatBytes(stats.NetworkTxBytes))
	if len(stats.Interfaces) > 0 {
		fmt.Fprintln(os.Stderr, "  per-interface:")
		for _, iface := range stats.Interfaces {
			fmt.Fprintf(os.Stderr, "    - %s rx=%s (%d pkts) tx=%s (%d pkts)\n",
				iface.Name,
				formatBytes(iface.RXBytes), iface.RXPackets,
				formatBytes(iface.TXBytes), iface.TXPackets,
			)
		}
	}
}

func formatPortForward(pf runtimectl.PortForward) string {
	proto := pf.Protocol
	if proto == "" {
		proto = runtimectl.PortProtocolTCP
	}
	hostIP := pf.HostIP
	if hostIP == "" {
		hostIP = "0.0.0.0"
	}
	desc := fmt.Sprintf("%s:%d -> %d/%s", hostIP, pf.HostPort, pf.GuestPort, proto)
	if pf.Description != "" {
		desc = fmt.Sprintf("%s (%s)", desc, pf.Description)
	}
	return desc
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func formatRelative(ts time.Time) string {
	if ts.IsZero() {
		return "n/a"
	}
	dur := time.Since(ts)
	if dur < 0 {
		dur = -dur
	}
	if dur < time.Second {
		return "<1s ago"
	}
	return fmt.Sprintf("%s ago", dur.Truncate(100*time.Millisecond))
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
