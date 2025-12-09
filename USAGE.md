# Quick Start Guide

## Automatic Agent Management

The agent now starts automatically and runs in the background at `~/.container/agent.sock`.

## Basic Usage

### Execute commands (simplest form)
```bash
# No flags needed - agent starts automatically
go run ./cmd/isolatectl cat file.txt
go run ./cmd/isolatectl ls -la
go run ./cmd/isolatectl echo "hello world"
```

### Default Behavior
- **Default socket**: `~/.container/agent.sock`
- **Default root directory**: Current working directory
- **Agent lifecycle**: Starts automatically, reuses existing agent, stops after command

### Custom Root Directory
```bash
# Restrict operations to a specific directory
go run ./cmd/isolatectl --root=./data cat file.txt
go run ./cmd/isolatectl --root=/tmp ls -la
```

### Disable Auto-Agent
```bash
# If you want to manage the agent manually
go run ./cmd/isolatectl --auto-agent=false --agent-unix=/path/to/socket.sock cat file.txt
```

## Examples

### Example 1: Read a file
```bash
$ go run ./cmd/isolatectl cat test.txt
[agent] started at /Users/user/.container/agent.sock (root: /Users/user/project)
test file
```

### Example 2: List files
```bash
$ go run ./cmd/isolatectl ls -la
[agent] started at /Users/user/.container/agent.sock (root: /Users/user/project)
total 72
drwxr-xr-x  15 user  staff   480 Dec  9 08:27 .
drwxr-xr-x  85 user  staff  2720 Dec  9 07:54 ..
-rw-r--r--   1 user  staff    10 Dec  9 08:27 test.txt
...
```

### Example 3: Path isolation (blocked)
```bash
$ go run ./cmd/isolatectl cat ../secret.txt
[agent] started at /Users/user/.container/agent.sock (root: /Users/user/project)
exec failed: security violation: argument 0 path "../secret.txt" escapes root "/Users/user/project"
```

### Example 4: Restrict to subdirectory
```bash
$ go run ./cmd/isolatectl --root=./data cat file.txt
[agent] started at /Users/user/.container/agent.sock (root: /Users/user/project/data)
file contents
```

## Manual Agent Management (Advanced)

If you need more control, you can still run the agent manually:

```bash
# Start agent with custom settings
go run ./cmd/agentd/main.go -unix /tmp/my-agent.sock -root /allowed/directory

# Use the custom agent
go run ./cmd/isolatectl --auto-agent=false --agent-unix=/tmp/my-agent.sock cat file.txt
```

## Important Notes

1. **Shell Commands**: When using the agent with isolation, use positional arguments instead of `--cmd` flag:
   - ✅ Good: `go run ./cmd/isolatectl cat file.txt`
   - ❌ Bad: `go run ./cmd/isolatectl --cmd="cat file.txt"` (bypasses isolation)

2. **Path Validation**: All file paths in command arguments are validated to prevent escaping the root directory

3. **Agent Reuse**: The agent stays running and is reused for subsequent commands within the same session

4. **Cleanup**: The agent stops when your terminal session ends or you can manually clean up with:
   ```bash
   rm -rf ~/.container
   ```
