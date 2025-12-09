#!/bin/bash
set -e

echo "=== Testing Container Isolation ==="
echo ""

# Setup
cd "$(dirname "$0")"
ROOT_DIR="$PWD/data"

echo "1. Setting up test environment..."
mkdir -p ./data ./data2 ./data/bin
echo "safe file" > ./data/safe.txt
echo "outside file" > ./data2/outside.txt

# Copy necessary binaries for chroot
if [ -x /bin/cat ]; then cp /bin/cat ./data/bin/cat; fi
if [ -x /bin/rm ]; then cp /bin/rm ./data/bin/rm; fi
if [ -x /bin/ls ]; then cp /bin/ls ./data/bin/ls; fi

echo "2. Starting agent with root restriction: $ROOT_DIR"
rm -f ./run/isolate/agent.sock
/usr/local/go/bin/go run ./cmd/agentd/main.go -unix ./run/isolate/agent.sock -root "$ROOT_DIR" -no-chroot &
AGENT_PID=$!
sleep 2

echo ""
echo "3. Testing operations INSIDE root directory (should work)..."
echo "   Listing ./data directory:"
/usr/local/go/bin/go run ./cmd/isolatectl --agent-unix="./run/isolate/agent.sock" "$ROOT_DIR/bin/ls" -la

echo ""
echo "4. Testing operations OUTSIDE root directory (should fail)..."
echo "   Attempting to read ../data2/outside.txt:"
if /usr/local/go/bin/go run ./cmd/isolatectl --agent-unix="./run/isolate/agent.sock" "$ROOT_DIR/bin/cat" ../data2/outside.txt 2>&1; then
    echo "   ❌ FAILED: Command should have been blocked!"
    kill $AGENT_PID 2>/dev/null
    exit 1
else
    echo "   ✅ SUCCESS: Command was blocked by isolation"
fi

echo ""
echo "5. Testing file deletion OUTSIDE root directory (should fail)..."
if /usr/local/go/bin/go run ./cmd/isolatectl --agent-unix="./run/isolate/agent.sock" "$ROOT_DIR/bin/rm" -rf ../data2/outside.txt 2>&1; then
    if [ -f ./data2/outside.txt ]; then
        echo "   ✅ SUCCESS: File deletion was blocked"
    else
        echo "   ❌ FAILED: File was deleted!"
        kill $AGENT_PID 2>/dev/null
        exit 1
    fi
else
    echo "   ✅ SUCCESS: Command was blocked by isolation"
fi

echo ""
echo "6. Cleaning up..."
kill $AGENT_PID 2>/dev/null
rm -f ./run/isolate/agent.sock

echo ""
echo "=== All isolation tests passed! ==="
