# Isolate: Cross-Platform VM Containers

A Go-based framework for executing commands inside fully isolated virtual
machines across Linux, Windows, and macOS. The system exposes a unified API that
selects the best-matching hypervisor on each platform (Firecracker, Cloud
Hypervisor, QEMU/KVM, Hyper-V, WSL2, Hypervisor.framework) while sharing a common
configuration surface for container management, command execution, and resource
limits.

## High-Level Architecture

```
┌──────────────────────────────────────────────────┐
│                 Client Application                │
│      (Go library consumers / isolatectl CLI)      │
└──────────────────────────┬───────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────┐
│                Unified API (pkg/isolate)         │
│ • Container lifecycle                            │
│ • Command execution + streaming                  │
│ • Resource + network configuration               │
│ • Metrics + status                               │
└──────────────────────────┬───────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────┐
│          Runtime Abstraction (pkg/isolate/runtime)│
│ • Hypervisor registry + selection                │
│ • VM + image management                          │
│ • Agent wiring + file transfer                   │
└───────────┬──────────────┬──────────────┬────────┘
            │              │              │
            ▼              ▼              ▼
     Linux Backends   Windows Backends  macOS Backends
     Firecracker      Hyper-V           Hypervisor.framework
     Cloud Hypervisor WSL2              QEMU (HVF)
     QEMU/KVM         QEMU              QEMU (fallback)
```

A lightweight guest agent abstraction (`pkg/isolate/agent`) defines the protocol
between host and guest. The current stub runtime uses a loopback agent for
development so the project builds end-to-end without shipping VM images yet.

## Packages

- `pkg/isolate`: Public API containing `Manager`, `Container` interface,
  configuration models, and result types.
- `pkg/isolate/runtime`: Runtime registry, VM interfaces, and stub
  implementations for Firecracker, Cloud Hypervisor, Hyper-V, WSL2, Hypervisor
  Framework, and QEMU.
- `pkg/isolate/agent`: Guest agent protocol, IPC client/server, loopback
  implementation, and transport helpers (unix sockets + vsock).
- `cmd/isolatectl`: Reference CLI showcasing runtime selection and command
  execution through the API.
- `cmd/agentd`: Minimal guest daemon exposing the agent protocol over unix
  sockets or vsock.

## Guest Agent and Metadata

`cmd/agentd` runs inside the guest (or alongside Firecracker microVMs) and wires
the IPC protocol to the OS process table. Start it with either a Unix domain
socket path or a vsock port:

```bash
# inside the guest VM (build or run from source)
go run ./cmd/agentd/main.go -unix /run/isolate/agent.sock

# or, on Linux guests that expose vsock to the host instead of unix sockets
go run ./cmd/agentd/main.go -vsock-port 10900
```

Host-side containers connect to the agent by setting metadata on the container
config (or via `isolatectl` flags):

```go
cfg := &isolate.Config{
    Name:   "build",
    CPUs:   4,
    Memory: 2 * 1024 * 1024 * 1024,
    Metadata: map[string]string{
        "agent.unix": "/run/isolate/agent.sock",
        // alternatively for vsock:
        // "agent.vsock.cid":  "3",
        // "agent.vsock.port": "10900",
    },
}
```

With metadata provided, the runtime automatically instantiates an IPC client and
falls back to the loopback or no-op client only when nothing else is available.

### Example: wiring the CLI to a guest agent

1. **Inside the guest VM** (or image template) run the agent:

  ```bash
  sudo mkdir -p /run/isolate
  sudo chown $USER /run/isolate
  go run ./cmd/agentd/main.go -unix /run/isolate/agent.sock
  ```

2. **On the host**, mount a project directory into the guest and run commands
  strictly inside that workspace:

  ```bash
  go run ./cmd/isolatectl/main.go \
    --agent-unix /run/isolate/agent.sock \
    --root "$PWD" \
    --workdir /workspace \
    --cmd "ls /workspace"
  ```

  The CLI bind-mounts `--root` at `--workdir` within the guest and executes the
  command through the agent. No host files are touched directly (loopback mode
  is disabled unless `--dev` is passed explicitly).

## Getting Started

```bash
# list runtimes available on your host
isolatectl -list

# run a command using the development loopback agent (executes on host)
isolatectl /bin/echo "hello from isolate"

# target a unix-socket agent exposed by a VM/guest
isolatectl -agent-unix /run/isolate/agent.sock /bin/hostname

# target a vsock agent (CID 3, port 10900)
isolatectl -agent-vsock-cid 3 -agent-vsock-port 10900 /bin/uname -a
```

## File Transfer

The unified API exposes `CopyTo` and `CopyFrom` on every container. With the
IPC agent in place, these stream file contents over the same transport used for
command execution:

```go
src, _ := os.Open("./build/output.tar.gz")
defer src.Close()

if err := container.CopyTo(ctx, src, "/tmp/output.tar.gz"); err != nil {
  log.Fatal(err)
}

var buf bytes.Buffer
if err := container.CopyFrom(ctx, "/etc/os-release", &buf); err != nil {
  log.Fatal(err)
}
fmt.Println(buf.String())
```

## Networking

Every container can opt into detailed networking controls via
`isolate.NetworkConfig`. Specify the operating mode (isolated/NAT/bridge), DNS
servers, port forwards, and even synthetic interfaces in a single struct:

```go
cfg := &isolate.Config{
  Name:   "api",
  CPUs:   4,
  Memory: 4 * 1024 * 1024 * 1024,
  NetworkMode: isolate.NetworkModeNAT, // legacy field still honored
  Network: &isolate.NetworkConfig{
    Mode:     isolate.NetworkModeBridge,
    Hostname: "api.vm",
    DNS:      []string{"1.1.1.1", "8.8.8.8"},
    PortForwards: []isolate.PortForward{
      {Protocol: isolate.PortProtocolTCP, HostPort: 8080, GuestPort: 80, Description: "HTTP"},
      {Protocol: isolate.PortProtocolUDP, HostPort: 5353, GuestPort: 5353},
    },
    Interfaces: []isolate.NetworkInterface{
      {Name: "eth1", SubnetCIDR: "10.52.0.0/24", IPv4: "10.52.0.10", Gateway: "10.52.0.1"},
    },
    Bandwidth: &isolate.BandwidthLimit{IngressBitsPerSec: 200 * 1000 * 1000},
  },
}
```

Concrete runtimes interpret these hints per-platform (e.g., invoking `iptables`
rules for NAT, configuring Hyper-V switches on Windows, or wiring Firecracker
microVMs via tap devices). The stub runtime stores the configuration so higher
layers can reason about desired topology during early development.

To integrate into your Go project:

```go
ctx := context.Background()
manager, err := isolate.NewDefaultManager()
if err != nil {
    log.Fatal(err)
}

container, err := manager.CreateContainer(ctx, &isolate.Config{
    Name:        "build-task",
    CPUs:        4,
    Memory:      2 * 1024 * 1024 * 1024,
    NetworkMode: runtimectl.NetworkModeNAT,
    DevMode:     true, // until a guest agent is connected
})
if err != nil {
    log.Fatal(err)
}

defer manager.DeleteContainer(ctx, "build-task")

if err := container.Start(ctx); err != nil {
    log.Fatal(err)
}

result, err := container.Exec(ctx, &isolate.Command{Path: "/bin/sh", Args: []string{"-c", "uname -a"}})
if err != nil {
    log.Fatal(err)
}

fmt.Printf("stdout: %s\n", result.Stdout)
```

## Next Steps

1. Replace the stub runtime with production-grade Firecracker/Hyper-V/
  Hypervisor.framework drivers that launch guest VMs directly.
2. Bundle signed guest images with `agentd` pre-installed and expose automated
  image import/downloading workflows.
3. Flesh out file transfer, mount propagation, network shaping, and persistent
  storage volumes at the agent/runtime boundary.
4. Add a persistent metadata store plus event streaming (stdout/stderr + audit)
  for long-running containers.


agentd can only bind to a Unix socket if the directory for that socket already exists and is writable. Your command is failing because /run/isolate hasn’t been created yet.

Inside the guest, run something like:

```shell
sudo mkdir -p /run/isolate
sudo chown "$USER" /run/isolate   # or grant whatever user will run agentd
go run ./cmd/agentd/main.go -unix /run/isolate/agent.sock
```

Once the directory exists, the listen unix call will succeed and isolatectl --agent-unix /run/isolate/agent.sock ... can talk to the guest safely.


isolatectl only talks to the guest when an agent endpoint is provided. In your run:

```shell
go run ./cmd/isolatectl/main.go --cmd="rm -rf file.txt" --workdir="./data"
```

two things are missing:

1) Agent connection – neither --agent-unix nor --agent-vsock-* was specified, so the runtime can’t reach a guest agent and errors with guest agent unavailable. Pass whichever endpoint your guest exposes, e.g.:

```shell
go run ./cmd/isolatectl/main.go \
  --agent-unix /run/isolate/agent.sock \
  --cmd "rm -rf file.txt" \
  --root "$PWD/data" \
  --workdir /workspace
```

2) Root mount – --workdir only sets the guest working directory. To give the guest access to your host data folder you also need --root <host-path> (added earlier). The CLI bind-mounts --root at --workdir before invoking the agent, so the command above removes /workspace/file.txt inside the guest, leaving the host untouched.

Once both the agent endpoint and root mount are provided, the command will run inside the guest VM against the mounted directory.
