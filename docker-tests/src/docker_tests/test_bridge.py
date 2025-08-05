#!/usr/bin/env python3

import asyncio
import uuid
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from irc_client import IRCClient
from ssh_chat_client import SSHChatClient


async def test_bidirectional_bridge() -> None:
    """Test bidirectional message bridging between IRC and SSH chat."""
    
    print("🚀 Starting matterbridge test...")
    
    test_id = str(uuid.uuid4())[:8]
    irc_nick = f"test_{test_id}"
    
    irc_client = IRCClient("localhost", 6667, irc_nick, irc_nick, "Test Client")
    ssh_client = SSHChatClient("localhost", 2022)
    
    try:
        # Connect and setup
        irc_client.connect()
        await ssh_client.connect()
        irc_client.join_channel("bridge-test")
        
        await asyncio.sleep(2)
        irc_client.read_messages(timeout=1.0)
        await ssh_client.read_messages(timeout=1.0)
        
        # Test 1: IRC → SSH Chat
        print("\n📨 IRC → SSH Chat")
        irc_client.send_message("bridge-test", f"Test message {test_id}")
        
        await asyncio.sleep(1.5)
        ssh_messages = await ssh_client.read_messages(timeout=2.0)
        
        irc_to_ssh_success = any(test_id in msg for msg in ssh_messages)
        print(f"{'✅' if irc_to_ssh_success else '❌'} IRC → SSH: {'SUCCESS' if irc_to_ssh_success else 'FAILED'}")
        
        # Test 2: SSH Chat → IRC  
        print("\n📨 SSH Chat → IRC")
        await ssh_client.send_message(f"Test message {test_id}_reverse")
        
        await asyncio.sleep(0.5)
        irc_messages = []
        for _ in range(2):
            await asyncio.sleep(0.5)
            irc_messages.extend(irc_client.read_messages(timeout=1.0))
        
        ssh_to_irc_success = any(f"{test_id}_reverse" in msg for msg in irc_messages)
        print(f"{'✅' if ssh_to_irc_success else '❌'} SSH → IRC: {'SUCCESS' if ssh_to_irc_success else 'FAILED'}")
        
        # Test 3: QUIT detection
        print("\n🚪 QUIT Detection")
        await ssh_client.read_messages(timeout=0.5)
        
        irc_client.disconnect()
        
        await asyncio.sleep(2)
        quit_messages = await ssh_client.read_messages(timeout=2.0)
        
        quit_detected = any(
            any(keyword in msg.lower() for keyword in ['quit', 'left', 'disconnected', 'part']) 
            for msg in quit_messages
        )
        
        print(f"{'✅' if quit_detected else '❌'} QUIT detection: {'SUCCESS' if quit_detected else 'FAILED'}")
        
        # Summary
        print(f"\n📊 Results: IRC→SSH {'✅' if irc_to_ssh_success else '❌'} | SSH→IRC {'✅' if ssh_to_irc_success else '❌'} | QUIT {'✅' if quit_detected else '❌'}")
        
        if not quit_detected:
            print("💡 QUIT events are not being bridged - this is the issue to fix in matterbridge")
        
    except ConnectionError as e:
        print(f"❌ Connection failed: {e}")
    except Exception as e:
        print(f"❌ Test error: {e}")
        
    finally:
        # Cleanup
        if hasattr(irc_client, 'connected') and irc_client.connected:
            try:
                irc_client.disconnect()
            except OSError:
                pass
                
        if hasattr(ssh_client, 'connected') and ssh_client.connected:
            try:
                await ssh_client.disconnect()
            except Exception:
                pass


if __name__ == "__main__":
    asyncio.run(test_bidirectional_bridge())
