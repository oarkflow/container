#!/bin/bash
# Quick demo of secure vs insecure mode

echo "=== Security Demo ==="
echo ""

# Cleanup
pkill -f agentd 2>/dev/null || true
rm -rf ~/.container 2>/dev/null || true

echo "1. INSECURE Mode (without root):"
echo "   The system allows execution but shows warnings..."
echo ""
timeout 5 go run ./cmd/isolatectl --root=./data python3 attack_test.py 2>&1 | head -12
echo ""

pkill -f agentd 2>/dev/null || true
rm -rf ~/.container 2>/dev/null || true
sleep 1

echo ""
echo "2. SECURE Mode (with root - requires sudo):"
echo "   Run this command to see chroot protection:"
echo ""
echo "   sudo go run ./cmd/agentd/main.go -unix /tmp/agent.sock -root $PWD/data &"
echo "   go run ./cmd/isolatectl --agent-unix=/tmp/agent.sock python3 attack_test.py"
echo ""
echo "   With chroot: All file access attempts are blocked!"
