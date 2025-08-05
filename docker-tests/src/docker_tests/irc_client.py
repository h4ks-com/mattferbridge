import asyncio
import socket
import time
from typing import Any


class IRCClient:
    """Basic IRC client for testing matterbridge connections."""
    
    def __init__(self, host: str, port: int, nickname: str, username: str = None, realname: str = None):
        self.host = host
        self.port = port
        self.nickname = nickname
        self.username = username or nickname
        self.realname = realname or nickname
        self.socket: socket.socket | None = None
        self.connected = False
        
    def connect(self) -> None:
        """Connect to IRC server and perform registration."""
        self.socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.socket.connect((self.host, self.port))
        self.connected = True
        
        # IRC registration sequence
        self._send_raw(f"NICK {self.nickname}")
        self._send_raw(f"USER {self.username} 0 * :{self.realname}")
        
        # Wait for registration to complete
        self._wait_for_registration()
    
    def disconnect(self) -> None:
        """Send QUIT and close connection."""
        if self.connected and self.socket:
            self._send_raw("QUIT :Test client disconnecting")
            self.socket.close()
            self.connected = False
    
    def join_channel(self, channel: str) -> None:
        """Join an IRC channel."""
        if not channel.startswith('#'):
            channel = f"#{channel}"
        self._send_raw(f"JOIN {channel}")
    
    def send_message(self, target: str, message: str) -> None:
        """Send a message to channel or user."""
        if not target.startswith('#'):
            target = f"#{target}"
        self._send_raw(f"PRIVMSG {target} :{message}")
    
    def read_messages(self, timeout: float = 5.0) -> list[str]:
        """Read available messages from IRC server."""
        messages = []
        if not self.socket:
            return messages
            
        self.socket.settimeout(timeout)
        
        try:
            while True:
                data = self.socket.recv(4096).decode('utf-8', errors='ignore')
                if not data:
                    break
                    
                for line in data.strip().split('\r\n'):
                    if line:
                        messages.append(line)
                        
                        # Respond to PING automatically  
                        if line.startswith('PING'):
                            pong_response = line.replace('PING', 'PONG', 1)
                            self._send_raw(pong_response.split(':', 1)[1])
                            
        except socket.timeout:
            pass
        except OSError:
            self.connected = False
            
        return messages
    
    def _send_raw(self, message: str) -> None:
        """Send raw IRC command."""
        if self.socket and self.connected:
            full_message = f"{message}\r\n"
            self.socket.send(full_message.encode('utf-8'))
    
    def _wait_for_registration(self) -> None:
        """Wait for IRC registration to complete."""
        registration_complete = False
        start_time = time.time()
        
        while not registration_complete and time.time() - start_time < 10:
            messages = self.read_messages(timeout=1.0)
            
            for message in messages:
                # Check for successful registration (001 welcome message)
                if ' 001 ' in message:
                    registration_complete = True
                    break
                # Handle nickname in use
                elif ' 433 ' in message:
                    self.nickname = f"{self.nickname}_test"
                    self._send_raw(f"NICK {self.nickname}")
        
        if not registration_complete:
            raise ConnectionError("IRC registration timed out")