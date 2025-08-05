import asyncio
import asyncssh
import time
from typing import Any


class SSHChatClient:
    """SSH Chat client for testing matterbridge connections."""
    
    def __init__(self, host: str, port: int):
        self.host = host
        self.port = port
        self.connection: asyncssh.SSHClientConnection | None = None
        self.process: asyncssh.SSHClientProcess | None = None
        self.connected = False
        self.messages: list[str] = []
    
    async def connect(self) -> None:
        """Connect to SSH chat server."""
        try:
            self.connection = await asyncssh.connect(
                self.host,
                port=self.port,
                known_hosts=None   # Skip host key verification for testing
            )
            
            self.process = await self.connection.create_process()
            self.connected = True
            
            await asyncio.sleep(1.0)
            
        except Exception as e:
            raise ConnectionError(f"Failed to connect to SSH chat: {e}")
    
    async def disconnect(self) -> None:
        """Disconnect from SSH chat."""
        if self.process:
            self.process.stdin.write("/quit\r\n")
            await self.process.stdin.drain()
            
        if self.connection:
            self.connection.close()
            await self.connection.wait_closed()
            
        self.connected = False
    
    async def send_message(self, message: str) -> None:
        """Send a message to SSH chat."""
        if self.process and self.connected:
            self.process.stdin.write(f"{message}\r\n")
            await self.process.stdin.drain()
    
    async def read_messages(self, timeout: float = 5.0) -> list[str]:
        """Read available messages from SSH chat."""
        messages = []
        if not self.process:
            return messages
        
        end_time = time.time() + timeout
        
        while time.time() < end_time:
            try:
                # Read with small timeout to allow multiple reads
                data = await asyncio.wait_for(
                    self.process.stdout.read(4096), 
                    timeout=0.5
                )
                
                if data:
                    # Handle both string and bytes data
                    if isinstance(data, bytes):
                        text = data.decode('utf-8', errors='ignore')
                    else:
                        text = str(data)
                    lines = text.strip().split('\n')
                    
                    for line in lines:
                        if line.strip():
                            clean_line = self._clean_ansi_codes(line)
                            if clean_line.strip():  # Only add non-empty lines
                                messages.append(clean_line)
                else:
                    # No data available, small sleep
                    await asyncio.sleep(0.1)
                        
            except asyncio.TimeoutError:
                # Continue reading until main timeout
                await asyncio.sleep(0.1)
            except (OSError, ConnectionError):
                self.connected = False
                break
            
        return messages
    
    async def wait_for_message_containing(self, text: str, timeout: float = 10.0) -> str | None:
        """Wait for a message containing specific text."""
        start_time = time.time()
        
        while time.time() - start_time < timeout:
            messages = await self.read_messages(timeout=1.0)
            
            for message in messages:
                if text in message:
                    return message
                    
            await asyncio.sleep(0.1)
            
        return None
    
    def _clean_ansi_codes(self, text: str) -> str:
        """Remove ANSI escape codes from text."""
        import re
        ansi_escape = re.compile(r'\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])')
        return ansi_escape.sub('', text)
