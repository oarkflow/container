#!/bin/bash
# Complete security test showing insecure vs secure mode

set -e

cd "$(dirname "$0")"

echo "=== Container Security Test Suite ==="
echo ""

# Cleanup
pkill -f agentd 2>/dev/null || true
rm -rf ~/.container /tmp/agent.sock 2>/dev/null || true
sleep 1

echo "TEST 1: Default Mode (Insecure without Root)"
echo "============================================="
echo "When not running as root, the system operates in INSECURE mode."
echo "This allows development/testing but scripts CAN escape isolation!"
echo ""

go run ./cmd/isolatectl --root=./data python3 attack_test.py 2>&1 | \
  grep -E "(WARNING|SUCCESS|BLOCKED|attempting)" | head -15

echo ""
echo "⚠️  Notice: Attacks SUCCEEDED because we're in insecure development mode!"
echo ""

# Cleanup
pkill -f agentd 2>/dev/null || true
rm -rf ~/.container 2>/dev/null || true
sleep 1

echo ""
echo "TEST 2: Secure Mode (With Root Privileges)"
echo "==========================================="
echo "With sudo, chroot isolation is enabled by default."
echo "This provides REAL security - all escape attempts should be blocked!"
echo ""
echo "Starting secure agent with chroot..."

# Start agent with sudo
sudo go run ./cmd/agentd/main.go -unix /tmp/agent.sock -root "$PWD/data" > /tmp/agent.log 2>&1 &
AGENT_PID=$!
sleep 3

echo "Running attack script with chroot protection..."
go run ./cmd/isolatectl --agent-unix=/tmp/agent.sock --auto-agent=false python3 attack_test.py 2>&1 | \
  grep -E "(SUCCESS|BLOCKED|attempting|permission denied)" | head -15

echo ""
echo "✓ All attacks should be BLOCKED with chroot!"
echo ""

# Cleanup
sudo kill $AGENT_PID 2>/dev/null || true
sudo rm -f /tmp/agent.sock /tmp/agent.log 2>/dev/null || true

echo ""
echo "=== Summary ==="
echo "• Without root: INSECURE mode (development only)"
echo "• With root: SECURE mode (chroot enabled by default)"
echo "• To force insecure mode: use --no-chroot flag"
echo "• For maximum security on Windows: use Docker/VMs (--no-agent mode)"
