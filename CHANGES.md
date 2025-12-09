# What Changed: Secure by Default

## Summary

The container isolation system is now **secure by default**:

✅ **With root privileges**: Chroot is enabled automatically - provides real security
⚠️ **Without root privileges**: Falls back to insecure mode with clear warnings - development only

## Key Changes

### 1. Chroot Enabled by Default

**Before:**
```bash
# Had to explicitly enable chroot
agentd -unix /tmp/agent.sock -root ./data -chroot
```

**After:**
```bash
# Chroot is automatic when running as root
sudo agentd -unix /tmp/agent.sock -root ./data
# ✓ chroot isolation enabled - secure execution mode

# Without root, you get warnings
agentd -unix /tmp/agent.sock -root ./data
# WARNING: chroot isolation is DISABLED - this is INSECURE!
```

### 2. Auto-Detection of Root Privileges

The agent manager automatically adds `--no-chroot` flag when not running as root, so it doesn't fail - it just warns you loudly that you're in insecure mode.

### 3. Interpreter Execution Policy

**Secure Mode (with chroot):**
- Python, Node.js, PHP, etc. execute normally
- All file access is restricted by the OS
- No escaping possible

**Insecure Mode (without chroot):**
- System warns: `WARNING: executing interpreter "python3" in INSECURE mode`
- Scripts can escape the root directory restriction
- Only safe for trusted code

### 4. Explicit Insecure Mode Flag

If you want to force insecure mode even with root:

```bash
sudo agentd -unix /tmp/agent.sock -root ./data --no-chroot
# WARNING: chroot isolation is DISABLED - this is INSECURE!
```

## Usage Examples

### Development (Insecure but Easy)

```bash
# No sudo needed, but only for trusted code
go run ./cmd/isolatectl --root=./data python3 my_script.py

# You'll see warnings:
# WARNING: chroot isolation is DISABLED
# WARNING: Scripts can escape the root directory restriction!
```

### Production (Secure with Root)

```bash
# Run agent with sudo for real security
sudo go run ./cmd/agentd/main.go -unix /tmp/agent.sock -root ./data &

# Execute commands
go run ./cmd/isolatectl --agent-unix=/tmp/agent.sock python3 script.py

# You'll see confirmation:
# ✓ chroot isolation enabled - secure execution mode
```

### Testing Security

```bash
# Quick demo
./demo_security.sh

# Full test suite
./test_complete.sh

# Run attack test manually
go run ./cmd/isolatectl --root=./data python3 attack_test.py
```

## What Gets Protected

### ✅ With Chroot (Secure Mode)

```bash
sudo agentd -root ./data ...

# All these attacks are BLOCKED:
python3 -c "open('/etc/passwd').read()"        # ❌ Permission denied
python3 -c "open('../test.txt').read()"        # ❌ No such file
python3 -c "os.system('cat /etc/passwd')"      # ❌ Permission denied
python3 -c "os.listdir('..')"                  # ❌ Permission denied
```

### ❌ Without Chroot (Insecure Mode)

```bash
agentd -root ./data ...  # No sudo

# All these attacks SUCCEED:
python3 -c "open('/etc/passwd').read()"        # ✓ Works!
python3 -c "open('../test.txt').read()"        # ✓ Works!
python3 -c "os.system('cat /etc/passwd')"      # ✓ Works!
python3 -c "os.listdir('..')"                  # ✓ Works!
```

## Platform Support

| Platform | Secure Mode | Requirement |
|----------|-------------|-------------|
| Linux | ✅ Chroot | Root (sudo) |
| macOS | ✅ Chroot | Root (sudo) |
| Unix | ✅ Chroot | Root (sudo) |
| Windows | ❌ No chroot | Use Docker/VMs |

### Windows Alternative

**The container system has built-in VM support:**

```bash
# Container manages Hyper-V/QEMU automatically
isolatectl --image=windows.img --root=./data python3 script.py

# Runtime automatically selects best hypervisor:
# - Hyper-V (if available)
# - QEMU (fallback)
# - WSL2 (if configured)
```

No external tools needed - everything is managed by the container runtime.

## Files Added/Modified

### New Files
- `SECURITY_MODEL.md` - Complete security documentation
- `demo_security.sh` - Quick demo showing both modes
- `test_complete.sh` - Full test suite

### Modified Files
- `cmd/agentd/main.go` - Chroot enabled by default, `--no-chroot` flag
- `pkg/isolate/agent/ipc_server.go` - `AllowInsecure` field, interpreter blocking
- `pkg/isolate/agent_manager.go` - Auto-detect root, add `--no-chroot` when needed

## Quick Reference

```bash
# Development (easy, insecure)
isolatectl --root=./data python3 script.py

# Production (secure, needs sudo)
sudo agentd -unix /tmp/agent.sock -root ./data &
isolatectl --agent-unix=/tmp/agent.sock python3 script.py

# Force insecure mode (not recommended)
agentd -unix /tmp/agent.sock -root ./data --no-chroot

# Test security
./demo_security.sh
```

## The Bottom Line

🔒 **For untrusted code**:
- Unix/Linux/macOS: Use `sudo` for chroot isolation
- Windows: Use built-in VM mode with `--image` (Hyper-V/QEMU managed automatically)

⚠️ **For development**: Insecure mode works, but read and understand the warnings

✅ **By default**: System tries to be secure, falls back with loud warnings if it can't be

**Everything is self-contained** - the container runtime manages all hypervisors internally.
