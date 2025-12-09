# Container Isolation

The isolation system prevents commands from accessing files outside a specified root directory.

## How It Works

1. **Agent Server**: When started with `--root`, the agent restricts all operations to that directory
2. **Path Validation**: All file paths in commands are checked to ensure they don't escape the root
3. **Shell Protection**: Shell commands (sh, bash, etc.) are blocked when isolation is enabled to prevent bypassing restrictions

## Usage

### Start Agent with Isolation

```bash
# Start the agent with a root directory restriction
go run ./cmd/agentd/main.go \
  -unix ./run/isolate/agent.sock \
  -root /path/to/allowed/directory
```

### Execute Commands

**✅ CORRECT - Use positional arguments:**
```bash
# Commands are checked for path escaping
go run ./cmd/isolatectl --agent-unix=./run/isolate/agent.sock cat file.txt
go run ./cmd/isolatectl --agent-unix=./run/isolate/agent.sock ls -la
go run ./cmd/isolatectl --agent-unix=./run/isolate/agent.sock rm file.txt
```

**❌ WRONG - Don't use --cmd flag with isolation:**
```bash
# The --cmd flag uses shell execution which bypasses isolation checks
go run ./cmd/isolatectl --agent-unix=./run/isolate/agent.sock --cmd="cat file.txt"
```

## Examples

### Example 1: Safe Operation (Allowed)
```bash
# Agent started with root=/data
go run ./cmd/agentd/main.go -unix ./agent.sock -root /data

# Reading a file inside /data - ALLOWED
go run ./cmd/isolatectl --agent-unix=./agent.sock cat file.txt
# → Works! Path resolves to /data/file.txt
```

### Example 2: Path Escape Attempt (Blocked)
```bash
# Agent started with root=/data
go run ./cmd/agentd/main.go -unix ./agent.sock -root /data

# Trying to read outside /data - BLOCKED
go run ./cmd/isolatectl --agent-unix=./agent.sock cat ../secrets.txt
# → Error: security violation: argument 0 path "../secrets.txt" escapes root "/data"
```

### Example 3: Absolute Path Outside Root (Blocked)
```bash
# Agent started with root=/data
go run ./cmd/agentd/main.go -unix ./agent.sock -root /data

# Trying to use absolute path outside root - BLOCKED
go run ./cmd/isolatectl --agent-unix=./agent.sock cat /etc/passwd
# → Error: argument 0 path "/etc/passwd" escapes root "/data"
```

## Security Features

- ✅ Blocks relative path traversal (e.g., `../`, `../../`)
- ✅ Blocks absolute paths outside root
- ✅ Validates working directory
- ✅ Checks all command arguments for file paths
- ✅ Prevents shell command execution that could bypass checks
- ✅ Automatically sets working directory to root if not specified

## Limitations

- Shell commands (`--cmd` flag) are blocked when isolation is enabled
- Symlinks are not currently validated (they could potentially escape)
- Commands must be executed directly (not through a shell)

## Testing

Run the test suite:
```bash
./test_isolation.sh
```

This tests:
1. Reading files inside the root (should work)
2. Reading files outside the root (should be blocked)
3. Deleting files outside the root (should be blocked)
