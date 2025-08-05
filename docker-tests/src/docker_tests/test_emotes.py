#!/usr/bin/env python3

import asyncio
import uuid
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from irc_client import IRCClient
from ssh_chat_client import SSHChatClient


async def test_emote_handling() -> None:
    """Test emote handling between IRC CTCP ACTION and SSH /me commands."""
    
    test_id = str(uuid.uuid4())[:8]
    irc_nick = f"emote_{test_id}"
    
    irc_client = IRCClient("localhost", 6667, irc_nick, irc_nick, "Emote Test Client")
    ssh_client = SSHChatClient("localhost", 2022)
    
    try:
        # Connect and setup
        irc_client.connect()
        await ssh_client.connect()
        irc_client.join_channel("bridge-test")
        
        await asyncio.sleep(2)
        irc_client.read_messages(timeout=1.0)
        await ssh_client.read_messages(timeout=1.0)
        
        # Test 1: IRC CTCP ACTION → SSH Chat
        action_message = f"tests emote functionality {test_id}"
        irc_client._send_raw(f"PRIVMSG #bridge-test :\x01ACTION {action_message}\x01")
        
        await asyncio.sleep(1.5)
        ssh_messages = await ssh_client.read_messages(timeout=2.0)
        
        action_received = any(test_id in msg for msg in ssh_messages)
        action_formatted_correctly = any(
            test_id in msg and "*" in msg and "tests emote functionality" in msg
            for msg in ssh_messages
        )
        
        # Test 2: SSH /me → IRC
        ssh_me_message = f"/me performs test action {test_id}_reverse"
        await ssh_client.send_message(ssh_me_message)
        
        await asyncio.sleep(0.5)
        irc_messages = []
        for _ in range(2):
            await asyncio.sleep(0.5)
            irc_messages.extend(irc_client.read_messages(timeout=1.0))
        
        me_received = any(f"{test_id}_reverse" in msg for msg in irc_messages)
        me_formatted_correctly = any(
            f"{test_id}_reverse" in msg and "*" in msg and "performs test action" in msg
            for msg in irc_messages
        )
        
        # Results
        print(f"IRC ACTION → SSH: {'✅' if action_received and action_formatted_correctly else '❌'}")
        print(f"SSH /me → IRC: {'✅' if me_received and me_formatted_correctly else '❌'}")
        
        if action_received and action_formatted_correctly and me_received and me_formatted_correctly:
            print("🎉 All emote formatting working correctly!")
        else:
            print("❌ Some emote formatting issues found")
        
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
    asyncio.run(test_emote_handling())