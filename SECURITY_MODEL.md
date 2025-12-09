# Security Model

This document explains the container isolation security model and how to use it safely.

## Overview

The system provides **three levels of isolation**:

1. **Path Validation** (Weak) - validates command-line arguments only
2. **Chroot** (Strong) - OS-level isolation via chroot syscall (Unix/Linux/macOS only)
3. **VM/Container** (Strongest) - complete virtualization (all platforms)

## Secure by Default

The system is **secure by default** when isolation is enabled:

### With Root Privileges (Recommended for Production)

```bash
# Chroot is ENABLED by default when running as root
sudo go run ./cmd/agentd/main.go -unix /tmp/agent.sock -root ./data &
go run ./cmd/isolatectl --agent-unix=/tmp/agent.sock python3 script.py
```

**Result:** Scripts are completely isolated - cannot access files outside root directory.

### Without Root Privileges (Development Only)

```bash
# Automatically falls back to INSECURE mode (with warnings)
go run ./cmd/isolatectl --root=./data python3 script.py
```

**Result:** System shows warnings but allows execution. Scripts CAN escape isolation!

**Warnings shown:**
```
WARNING: chroot isolation is DISABLED - this is INSECURE for untrusted code!
WARNING: Scripts can escape the root directory restriction!
WARNING: Only use --no-chroot for development with trusted code!
```

## Security Guarantees

### ✅ What IS Protected (with Chroot or VM)

- **File Access:** Scripts cannot read/write files outside root directory
- **Process Isolation:** Cannot see or interact with host processes
- **System Calls:** Restricted to safe operations within the container
- **Network:** Can be isolated (depending on configuration)

### ❌ What Is NOT Protected (without Chroot)

Path validation only checks **command-line arguments**, not script contents:

```bash
# This is blocked (argument validation)
isolatectl --root=./data cat /etc/passwd
# Error: path "/etc/passwd" outside root directory

# This is NOT blocked (script contains the attack)
cat > attack.py << 'EOF'
with open('/etc/passwd', 'r') as f:
    print(f.read())
EOF

isolatectl --root=./data python3 attack.py
# Works! Python script can access any file
```

**Why?** The system sees `python3 attack.py` - both are valid paths. It cannot inspect what's *inside* `attack.py`.

## Platform-Specific Behavior

### Linux & macOS

```bash
# Secure mode (requires root)
sudo isolatectl --root=./data python3 script.py

# Insecure mode (development only)
isolatectl --root=./data --no-chroot python3 script.py
```

### Windows

Windows doesn't support chroot. The container system automatically uses built-in VM isolation:

```bash
# VM mode (uses built-in Hyper-V or QEMU backend)
isolatectl --image=windows.img --root=./data python3 script.py

# The container runtime automatically selects:
# - Hyper-V (if available and enabled)
# - QEMU (fallback)
# - WSL2 (if configured)
```

## When to Use Each Mode

### Use Chroot (Default with Root)

✅ **For:** Running untrusted code safely
✅ **Platforms:** Linux, macOS, Unix
✅ **Requirement:** Root privileges
✅ **Security:** Strong - real OS-level isolation

```bash
sudo isolatectl --root=./data python3 untrusted_script.py
```

### Use Insecure Mode (Development)

⚠️ **For:** Development and testing only
⚠️ **Trust:** ONLY run code you trust completely
⚠️ **Requirement:** No special privileges needed
⚠️ **Security:** Weak - scripts can escape

```bash
isolatectl --root=./data python3 my_trusted_script.py
```

### Use VM Mode (Maximum Security)

🔒 **For:** Maximum security, Windows, or when root unavailable
🔒 **Platforms:** All (Linux, Windows, macOS)
🔒 **Requirement:** VM image (container builds/manages hypervisor)
🔒 **Security:** Strongest - complete isolation

```bash
# Container system manages VM automatically
isolatectl --image=vm.img --root=./data python3 script.py
```

## Testing Security

Run the test suite to verify isolation:

```bash
# Test 1: Show insecure mode warnings
./test_complete.sh

# Test 2: Verify chroot protection (requires sudo)
sudo go run ./cmd/agentd/main.go -unix /tmp/agent.sock -root ./data &
go run ./cmd/isolatectl --agent-unix=/tmp/agent.sock python3 attack_test.py
# Should see: All attacks BLOCKED
```

## Best Practices

### ✅ DO

- Run with `sudo` on Unix/Linux/macOS for chroot protection
- Use VM mode (built-in) for Windows or when root unavailable
- Read all warnings and understand the risks
- Test with `attack_test.py` to verify isolation works
- Let the container runtime manage VMs automatically

### ❌ DON'T

- Run untrusted code without chroot/VM
- Ignore security warnings
- Use `--no-chroot` in production
- Assume path validation alone is secure
- Mix trusted and untrusted code in the same root

## FAQ

**Q: Why does my Python script read /etc/passwd despite "isolation"?**

A: You're running in insecure mode (no root). Path validation only checks arguments, not script contents. Use `sudo` for real security.

**Q: Can I run securely on Windows?**

A: Yes! The container system has built-in VM support for Windows using Hyper-V or QEMU. Just provide a VM image with `--image`.

**Q: What if I can't use sudo?**

A: Then you're limited to insecure mode. Only run code you fully trust, or use Docker/VMs instead.

**Q: How do I know which mode I'm in?**

A: Check the logs:
- `✓ chroot isolation enabled` = SECURE
- `WARNING: chroot isolation is DISABLED` = INSECURE

**Q: Is this safe for running CI/CD jobs?**

A: Yes! Use sudo (Unix/Linux/macOS) or VM mode with `--image` (all platforms). Don't use insecure mode for CI/CD.

## Summary

The system is **secure by default when running as root**. It automatically:

1. ✅ Enables chroot isolation when running with `sudo`
2. ⚠️ Falls back to insecure mode without root (with clear warnings)
3. ❌ Refuses to run interpreters without chroot UNLESS you explicitly allow it

For production: Always use `sudo` (Unix/Linux/macOS) or VM mode with `--image` (all platforms).
