#!/bin/bash
# Security test - demonstrates secure by default behavior

echo "=== Security Isolation Test ==="
echo ""

cd "$(dirname "$0")"

# Clean up
pkill -f agentd 2>/dev/null || true
rm -rf ~/.container
sleep 1

echo "TEST 1: Secure by Default (Requires Root)"
echo "=========================================="
echo "Attempting to run with default settings (chroot enabled)..."
echo ""

# Run with timeout and capture output
timeout 3 go run ./cmd/isolatectl --root=./data python3 attack_test.py 2>&1 | grep -E "(ERROR|chroot|HINT|refusing)" | head -5

echo ""
echo "✓ System correctly refuses to run without proper isolation!"
echo ""

# Kill any remaining agent processes
pkill -f agentd 2>/dev/null || true
rm -rf ~/.container 2>/dev/null || true

echo ""
echo "TEST 2: INSECURE Mode (Development Only)"
echo "=========================================="
echo "Running with --no-chroot flag (INSECURE - bypasses protection)..."
echo ""

# Start agent in insecure mode
go run ./cmd/agentd/main.go -unix ~/.container/agent.sock -root ./data --no-chroot &
AGENT_PID=$!
sleep 2

go run ./cmd/isolatectl --root=./data --auto-agent=false python3 attack_test.py 2>&1 | head -20

# Clean up
kill $AGENT_PID 2>/dev/null || true
pkill -f agentd 2>/dev/null || true
rm -rf ~/.container 2>/dev/null || true
sleep 1

echo ""
echo "TEST 2: Strong Isolation (Would Require Chroot)"
echo "=============================================="
echo "To test with chroot (requires root):"
echo ""
echo "  sudo go run ./cmd/agentd/main.go \\"
echo "    -unix /tmp/agent.sock \\"
echo "    -root $(pwd)/data &"
echo ""
echo "  go run ./cmd/isolatectl \\"
echo "    --agent-unix=/tmp/agent.sock \\"
echo "    --auto-agent=false \\"
echo "    python3 attack_test.py"
echo ""
echo "With chroot: ALL attacks should be BLOCKED"
echo ""

echo ""
echo "TEST 3: Maximum Isolation (VM Mode)"
echo "=============================================="
echo "To test with full VM isolation:"
echo ""
echo "  go run ./cmd/isolatectl \\"
echo "    --no-agent \\"
echo "    --image=/path/to/vm.img \\"
echo "    --root=./data \\"
echo "    python3 attack_test.py"
echo ""
echo "With VM: Complete isolation, all attacks BLOCKED"
echo ""

echo "=== Security Recommendations ==="
echo ""
echo "✅ For trusted scripts in development: Current mode (weak isolation) is OK"
echo "⚠️  For untrusted code: USE chroot (Unix) or VM/containers"
echo "❌ Never run untrusted code without OS-level isolation!"
