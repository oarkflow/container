#!/bin/bash
# Demo script for automatic agent management

echo "=== Automatic Agent Management Demo ==="
echo ""

cd "$(dirname "$0")"

# Clean up any previous agent
rm -rf ~/.container
pkill -f "agentd" 2>/dev/null || true
sleep 1

echo "1. Simple command - agent starts automatically"
echo "   Command: go run ./cmd/isolatectl cat test.txt"
go run ./cmd/isolatectl cat test.txt
echo ""

echo "2. List files - reuses existing agent"
echo "   Command: go run ./cmd/isolatectl ls -la | grep test"
go run ./cmd/isolatectl ls -la | grep test
echo ""

echo "3. Try to access parent directory - BLOCKED"
echo "   Command: go run ./cmd/isolatectl cat ../README.md"
if go run ./cmd/isolatectl cat ../README.md 2>&1 | grep -q "security violation"; then
    echo "   ✅ Access blocked correctly"
else
    echo "   ❌ Should have been blocked!"
fi
echo ""

echo "4. Access within root directory - ALLOWED"
echo "   Command: go run ./cmd/isolatectl cat test.txt"
go run ./cmd/isolatectl cat test.txt
echo ""

echo "5. Using --root flag to restrict to subdirectory"
echo "   Command: go run ./cmd/isolatectl --root=./data ls -la"
go run ./cmd/isolatectl --root=./data ls -la 2>/dev/null | head -5
echo ""

echo "=== Demo Complete ==="
echo ""
echo "Key features:"
echo "  ✅ Agent starts automatically"
echo "  ✅ Agent is reused for multiple commands"
echo "  ✅ Default socket: ~/.container/agent.sock"
echo "  ✅ Default root: current directory"
echo "  ✅ Path isolation enforced"
echo "  ✅ No manual agent management needed"
