# Local Test Environment for Matterbridge

This Docker Compose setup provides a local testing environment with:
- **Ergo IRC Server** - Modern IRC server
- **SSH Chat** - Terminal-based chat server  
- **Matterbridge** - Bridges messages between IRC and SSH Chat

## Quick Start

1. **Start the environment:**
   ```bash
   docker compose up -d
   ```

2. **Connect to IRC:**
   ```bash
   # Using any IRC client
   irc://localhost:6667
   # Join channel: #bridge-test
   ```

3. **Connect to SSH Chat:**
   ```bash
   ssh -p 2022 localhost
   ```

## Services

### Ergo IRC (Port 6667)
- Server: `irc.testnet.local`
- Test channel: `#bridge-test`
- Admin user: `admin` (password: `admin123`)

### SSH Chat (Port 2022)
- Connect via SSH on port 2022
- Admin key located at `docker-configs/sshchat/admin_key`

### Matterbridge
- Automatically bridges messages between IRC `#bridge-test` and SSH chat
- Bot nickname: `bridge-bot`

## Testing

1. Send a message in IRC `#bridge-test` channel
2. It should appear in SSH chat
3. Send a message in SSH chat  
4. It should appear in IRC channel

## Configuration Files

- `docker-configs/ergo/ircd.yaml` - Ergo IRC server configuration
- `docker-configs/matterbridge/matterbridge.toml` - Bridge configuration
- `docker-configs/sshchat/` - SSH chat admin key and MOTD

## Logs

View matterbridge logs:
```bash
docker compose logs -f matterbridge
```

View all service logs:
```bash
docker compose logs -f
```

## Cleanup

```bash
docker compose down
```