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
    
    print("🎭 Starting emote handling test...")
    
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
        print("\n🎭 IRC CTCP ACTION → SSH Chat")
        print("💡 Should show as '*action message*' in SSH Chat")
        
        # Send IRC ACTION using raw CTCP command
        action_message = f"tests emote functionality {test_id}"
        irc_client._send_raw(f"PRIVMSG #bridge-test :\x01ACTION {action_message}\x01")
        
        await asyncio.sleep(1.5)
        ssh_messages = await ssh_client.read_messages(timeout=2.0)
        
        print(f"SSH messages received: {len(ssh_messages)}")
        for msg in ssh_messages:
            print(f"  SSH: {msg}")
        
        # Check if action is formatted with asterisks
        action_formatted_correctly = any(
            test_id in msg and "*" in msg and "tests emote functionality" in msg
            for msg in ssh_messages
        )
        
        # Also check if it's received at all
        action_received = any(test_id in msg for msg in ssh_messages)
        
        print(f"{'✅' if action_received else '❌'} IRC ACTION received: {'YES' if action_received else 'NO'}")
        print(f"{'✅' if action_formatted_correctly else '❌'} IRC ACTION formatted with *asterisks*: {'YES' if action_formatted_correctly else 'NO - This is the issue to fix'}")
        
        # Test 2: SSH /me → IRC
        print("\n🎭 SSH /me → IRC")  
        print("💡 Should show as '*action message*' in IRC and be sent at all")
        
        # Send /me command in SSH chat
        ssh_me_message = f"/me performs test action {test_id}_reverse"
        await ssh_client.send_message(ssh_me_message)
        
        await asyncio.sleep(0.5)
        irc_messages = []
        for _ in range(2):
            await asyncio.sleep(0.5)
            irc_messages.extend(irc_client.read_messages(timeout=1.0))
        
        print(f"IRC messages received: {len(irc_messages)}")
        for msg in irc_messages:
            print(f"  IRC: {msg}")
        
        # Check if /me message is sent to IRC at all
        me_received = any(f"{test_id}_reverse" in msg for msg in irc_messages)
        
        # Check if it's formatted with asterisks when received
        me_formatted_correctly = any(
            f"{test_id}_reverse" in msg and "*" in msg and "performs test action" in msg
            for msg in irc_messages
        )
        
        print(f"{'✅' if me_received else '❌'} SSH /me received in IRC: {'YES' if me_received else 'NO - This is the main issue to fix'}")
        print(f"{'✅' if me_formatted_correctly else '❌'} SSH /me formatted with *asterisks*: {'YES' if me_formatted_correctly else 'NO - This is also an issue to fix'}")
        
        # Summary
        print(f"\n📊 Emote Test Results:")
        print(f"  IRC ACTION → SSH: Received {'✅' if action_received else '❌'} | Formatted {'✅' if action_formatted_correctly else '❌'}")
        print(f"  SSH /me → IRC: Received {'✅' if me_received else '❌'} | Formatted {'✅' if me_formatted_correctly else '❌'}")
        
        overall_success = action_formatted_correctly and me_received and me_formatted_correctly
        
        if not overall_success:
            print("\n💡 Issues found:")
            if not action_formatted_correctly:
                print("  - IRC CTCP ACTION messages should be wrapped in *asterisks* when bridged")
            if not me_received:
                print("  - SSH /me commands are not being sent to IRC at all")  
            if not me_formatted_correctly and me_received:
                print("  - SSH /me commands should be wrapped in *asterisks* when bridged")
            
            print("\n🔧 Next steps:")
            print("  - Modify matterbridge IRC bridge to format EventUserAction with *asterisks*")
            print("  - Modify matterbridge SSH chat bridge to detect /me commands and send as EventUserAction")
            print("  - Ensure both directions show emotes consistently as *action message*")
        else:
            print("\n🎉 All emote formatting is working correctly!")
        
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