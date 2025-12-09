# Container Isolation - Complete Guide

## Quick Summary

**⚠️ IMPORTANT:** Path validation alone **CANNOT** protect against script-based attacks. Scripts in Python, PHP, Node.js, Bash, etc. can bypass argument validation.

## Three Execution Modes

### 1. Agent Mode with Path Validation (DEFAULT - Weak Security)
```bash
go run ./cmd/isolatectl python script.py
```
- ✅ Fast and convenient
- ✅ Good for development with trusted scripts
- ❌ Scripts can escape isolation
- ❌ NOT secure for untrusted code

### 2. Agent Mode with Chroot (Strong Security - Unix Only)
```bash
# Requires root privileges
sudo go run ./cmd/agentd/main.go -unix /tmp/agent.sock -root /workspace -chroot &
go run ./cmd/isolatectl --agent-unix=/tmp/agent.sock --auto-agent=false python script.py
```
- ✅ OS-level isolation
- ✅ Prevents ALL file access outside root
- ✅ Secure for untrusted code
- ❌ Requires root
- ❌ Unix/Linux/macOS only

### 3. VM Mode (Maximum Security - All Platforms)
```bash
go run ./cmd/isolatectl --no-agent --image=/path/to/vm.img python script.py
```
- ✅ Complete isolation
- ✅ Works on Windows
- ✅ Network isolation possible
- ❌ Slower (VM overhead)
- ❌ Requires VM image

## Usage Examples

### Development (Trusted Code)
```bash
# Simple command
go run ./cmd/isolatectl ls -la

# Python script
go run ./cmd/isolatectl python3 myscript.py

# With custom root
go run ./cmd/isolatectl --root=./workspace node app.js
```

### Production (Untrusted Code on Unix/Linux)
```bash
# Start agent as root with chroot
sudo go run ./cmd/agentd/main.go \
  -unix /var/run/agent.sock \
  -root /var/lib/isolated \
  -chroot &

# Execute as normal user
go run ./cmd/isolatectl \
  --agent-unix=/var/run/agent.sock \
  --auto-agent=false \
  python3 untrusted.py
```

### Production (Untrusted Code on Windows)
```bash
# Use Docker
docker run --rm \
  -v $(pwd)/workspace:/work \
  -w /work \
  --network none \
  python:3.9 python untrusted.py

# Or use WSL2 with chroot
wsl -d Ubuntu -- sudo /path/to/agentd -unix /tmp/agent.sock -root /workspace -chroot
```

### VM Mode (Any Platform)
```bash
# Full isolation
go run ./cmd/isolatectl \
  --no-agent \
  --image=/images/ubuntu.img \
  --root=/workspace \
  --memory=$((2*1024*1024*1024)) \
  python3 script.py
```

## Security Test

Run the security test to see the difference:
```bash
./test_security.sh
```

This demonstrates how scripts can bypass path validation but are blocked by chroot/VM isolation.

## Platform Compatibility

| Feature | Linux | macOS | Windows |
|---------|-------|-------|---------|
| Path Validation | ✅ | ✅ | ✅ |
| Chroot | ✅ | ✅ | ❌ |
| VM Mode | ✅ | ✅ | ✅ |
| Docker/Containers | ✅ | ✅ | ✅ |

## When to Use What

| Scenario | Recommended Mode |
|----------|------------------|
| Development with your own code | Path Validation (default) |
| Running user-submitted scripts | Chroot (Unix) or VM/Container |
| Windows production | VM or Docker |
| Maximum paranoia | VM with network isolation |
| CI/CD pipelines | Docker containers |

## Important Security Notes

1. **Path validation is NOT secure** against scripts
2. **Always use chroot or VM** for untrusted code
3. **On Windows, use Docker/containers** (no chroot)
4. **Test your security** with `test_security.sh`
5. **Read SECURITY.md** for detailed threat model

## Files

- `SECURITY.md` - Detailed security model and threats
- `USAGE.md` - Quick start guide
- `test_security.sh` - Security demonstration
- `data/attack_test.py` - Example attack script

## Command Reference

```bash
# Agent daemon
go run ./cmd/agentd/main.go -unix <socket> -root <dir> [-chroot]

# Execute commands
go run ./cmd/isolatectl [--root=<dir>] <command> [args...]
go run ./cmd/isolatectl --no-agent --image=<img> <command> [args...]

# Flags
--root=<dir>          # Root directory for isolation
--no-agent            # Disable agent, use VM mode
--auto-agent=false    # Don't start agent automatically
--chroot              # Enable chroot (agentd only, requires root)
```
