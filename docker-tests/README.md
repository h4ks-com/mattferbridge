# Matterbridge Test Suite

Python test scripts for testing matterbridge IRC ” SSH Chat bridging functionality.

## Setup

This project uses `uv` for dependency management. The main dependency is `asyncssh` for SSH Chat client functionality.

## Components

### IRCClient (`irc_client.py`)
- Basic IRC client implementation using raw TCP sockets
- Handles registration, channel joining, message sending
- Automatically responds to PING messages
- Supports QUIT detection testing
- Uses modern Python type hints (3.12+ syntax)

### SSHChatClient (`ssh_chat_client.py`) 
- SSH Chat client using asyncssh library
- Uses your `~/.ssh` keys for authentication (mounted read-only in Docker)
- Handles ANSI code cleanup from chat output
- Supports message waiting and filtering
- Fully async implementation

### Bridge Test (`test_bridge.py`)
- Tests bidirectional message bridging between IRC and SSH Chat
- **Main focus**: Tests IRC QUIT detection
- Currently, matterbridge doesn't bridge IRC QUIT messages as part events
- The test identifies this gap for fixing in matterbridge
- Generates unique test IDs to avoid conflicts

## Current Issue

The test reveals that when an IRC client sends `QUIT`, matterbridge doesn't translate this to a "part" event visible in SSH Chat. This makes it appear as if users just vanish without notice.

## Expected Behavior

IRC QUIT messages should be converted to part events that show up in SSH Chat as user leave notifications, similar to how JOIN messages are bridged.

## Usage

1. Make sure your Docker Compose environment is running:
   ```bash
   docker compose up -d
   ```

2. Run the test from the docker-tests directory:
   ```bash
   cd docker-tests
   uv run src/docker_tests/test_bridge.py
   ```

3. Alternative: Run as a module:
   ```bash
   uv run -m docker_tests.test_bridge
   ```

## Test Output

The test will show:
- Connection logs for both IRC and SSH Chat
- Message exchange logs with directional indicators (`>>` outgoing, `<<` incoming)
- Success/failure status for each test case
- Specific identification of the QUIT detection issue

## Next Steps

Based on test results, the matterbridge IRC bridge code needs modification to:
1. Detect IRC QUIT messages 
2. Convert them to part events with `ShowJoinPart=true`
3. Bridge these events to other protocols like SSH Chat