# Security Model & Isolation

## Problem: Script-Based Attacks

Any script in ANY language (Python, PHP, Node.js, Go, Ruby, Perl, etc.) can bypass simple argument-based path validation:

### Example Attacks

**Python:**
```python
# malicious.py
import os
os.system('rm -rf /important/data')
open('/etc/passwd', 'r').read()
```

**PHP:**
```php
<?php
// malicious.php
unlink('/important/data/file.txt');
readfile('/etc/passwd');
?>
```

**Node.js:**
```javascript
// malicious.js
const fs = require('fs');
fs.unlinkSync('/important/data/file.txt');
fs.readFileSync('/etc/passwd');
```

**Bash:**
```bash
#!/bin/bash
rm -rf /important/data
cat /etc/passwd
```

## Current Security Layers

### Layer 1: Argument Path Validation (WEAK)
- Validates file paths in command arguments
- **Cannot prevent:** Scripts that contain arbitrary file operations
- **Protection Level:** Low - only catches direct file path arguments

### Layer 2: Chroot Isolation (STRONG - Unix/Linux/macOS only)
- Uses OS-level `chroot` to jail the process
- **Prevents:** ANY file access outside the root directory
- **Requirements:**
  - Root privileges (or CAP_SYS_CHROOT capability)
  - Unix-like OS (Linux, macOS, BSD)
  - Not available on Windows
- **Protection Level:** High - OS-enforced isolation

### Layer 3: VM/Container Isolation (STRONGEST)
- Full VM or container isolation
- **Prevents:** ALL access to host filesystem
- **Works on:** All platforms (Windows, Linux, macOS)
- **Protection Level:** Maximum - complete isolation

## Recommended Usage

### 1. For Development/Testing (Low Security)
```bash
# Basic path validation only - NOT SECURE against scripts
go run ./cmd/isolatectl --root=./data python script.py
```
**⚠️ WARNING:** Scripts can escape! Do NOT use with untrusted code.

### 2. For Production (High Security - Unix/Linux)
```bash
# Run agent as root with chroot enabled
sudo go run ./cmd/agentd/main.go \
  -unix /var/run/agent.sock \
  -root /isolated/workspace \
  -chroot

# Then connect from non-root user
go run ./cmd/isolatectl \
  --agent-unix=/var/run/agent.sock \
  --auto-agent=false \
  python script.py
```
**✅ SECURE:** OS-level chroot prevents all escapes.

### 3. For Maximum Security (All Platforms)
```bash
# Use full VM mode
go run ./cmd/isolatectl \
  --no-agent \
  --image=/path/to/vm/image \
  --root=/workspace \
  python script.py
```
**✅ MOST SECURE:** Complete VM isolation.

### 4. For Windows (Container Required)
```bash
# Windows doesn't support chroot - use Docker/WSL2
docker run -v $(pwd):/workspace -w /workspace \
  --network none \
  python:3.9 python script.py

# Or use WSL2 with the agent running inside
wsl -d Ubuntu -- /path/to/agentd -unix /tmp/agent.sock -root /workspace
```

## Security Matrix

| Execution Mode | Script Protection | Platform | Requires Root | Security Level |
|----------------|-------------------|----------|---------------|----------------|
| Direct (no isolation) | ❌ None | All | No | 🔴 None |
| Path validation only | ❌ Weak | All | No | 🟡 Low |
| Chroot | ✅ Strong | Unix | Yes | 🟢 High |
| VM | ✅ Complete | All | No | 🟢 Maximum |
| Container (Docker) | ✅ Complete | All | No | 🟢 Maximum |

## When to Use Each Mode

### Use Path Validation (Default) When:
- ✅ Running your own trusted scripts
- ✅ Development and testing
- ✅ Scripts don't perform file operations
- ❌ NOT for untrusted code
- ❌ NOT for production

### Use Chroot When:
- ✅ Running untrusted scripts
- ✅ Production on Unix/Linux/macOS
- ✅ Need strong isolation
- ✅ Have root access
- ❌ NOT on Windows

### Use VM/Container When:
- ✅ Running untrusted code
- ✅ Cross-platform support needed
- ✅ Maximum security required
- ✅ Windows support needed
- ✅ Network isolation needed

## Important Notes

1. **Path validation alone is NOT secure** against script-based attacks
2. **Chroot requires root privileges** on most systems
3. **Windows does not support chroot** - use Docker/containers instead
4. **Always use VM/container mode** for untrusted code on Windows
5. **Scripts can do ANYTHING** the process can do - only OS-level isolation helps

## Example: Securing Python Execution

```bash
# ❌ INSECURE - script can escape
go run ./cmd/isolatectl --root=./workspace python malicious.py

# ✅ SECURE - chroot prevents escape (Unix/Linux only)
sudo go run ./cmd/agentd/main.go -unix /tmp/agent.sock -root /workspace -chroot &
go run ./cmd/isolatectl --agent-unix=/tmp/agent.sock --auto-agent=false python malicious.py

# ✅ SECURE - VM isolation (all platforms)
go run ./cmd/isolatectl --no-agent --image=ubuntu.img python malicious.py

# ✅ SECURE - Docker (all platforms)
docker run --rm -v $(pwd)/workspace:/work -w /work --network none python:3.9 python malicious.py
```

## Conclusion

**For untrusted code execution, you MUST use:**
1. Chroot (Unix/Linux/macOS with root)
2. VM isolation
3. Container (Docker/Podman)

**Path validation alone is insufficient** and should only be used for trusted scripts in development.
