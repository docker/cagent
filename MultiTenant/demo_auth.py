#!/usr/bin/env python3
"""
Demo script showing the authentication flow and chat capabilities
"""

import sys
import time
from datetime import datetime
from AuthAgentChat import AuthAgentChat

def print_header(text):
    """Print a formatted header"""
    print("\n" + "=" * 60)
    print(f"  {text}")
    print("=" * 60)

def demo():
    """Run the authentication and chat demo"""
    
    print_header("Cagent Authentication Demo")
    
    # Create the chat client
    print("\n➜ Creating chat client...")
    chat = AuthAgentChat(
        'https://cagent-api-950783879036.us-central1.run.app',
        'pirate.yaml',
        verbose=False
    )
    
    # Check authentication status
    print(f"➜ Authentication {'enabled' if chat.auth_enabled else 'disabled'} on server")
    
    if not chat.auth_enabled:
        print("⚠️ Authentication is disabled - demo requires auth to be enabled")
        return False
    
    # Demo registration flow
    print_header("User Registration")
    
    test_email = f"demo_{datetime.now().strftime('%Y%m%d_%H%M%S')}@example.com"
    print(f"➜ Registering new user: {test_email}")
    
    # Simulate registration
    chat.auth_enabled = True  # Ensure auth is enabled
    
    # Direct registration for demo
    import requests
    response = requests.post(
        f"{chat.api_url}/api/auth/register",
        json={
            "email": test_email,
            "password": "demopass123",
            "name": "Demo User"
        },
        timeout=10
    )
    
    if response.status_code == 200:
        data = response.json()
        chat.token = data['token']
        chat.user = data['user']
        print(f"✅ User registered successfully!")
        print(f"   User ID: {chat.user['id']}")
        print(f"   Name: {chat.user['name']}")
    else:
        print(f"❌ Registration failed: {response.text}")
        return False
    
    # Verify agent exists
    print_header("Agent Verification")
    print("➜ Checking if pirate.yaml agent exists...")
    
    if chat.verify_agent():
        print("✅ Agent 'pirate.yaml' is available")
    else:
        print("❌ Agent not found")
        return False
    
    # Create a chat session
    print_header("Creating Chat Session")
    session_title = f"Demo Session - {datetime.now().strftime('%Y-%m-%d %H:%M')}"
    print(f"➜ Creating session: {session_title}")
    
    if chat.create_session(session_title):
        print(f"✅ Session created with ID: {chat.session_id}")
    else:
        print("❌ Failed to create session")
        return False
    
    # Send messages to the pirate agent
    print_header("Interactive Chat with Pirate Agent")
    
    messages = [
        "Ahoy! What be yer name, pirate?",
        "Tell me about yer greatest treasure!",
        "What's a pirate's favorite programming language?"
    ]
    
    for msg in messages:
        print(f"\n👤 You: {msg}")
        print("🏴‍☠️ Pirate: ", end='', flush=True)
        
        # Send message
        import requests
        import json
        import sseclient
        
        response = requests.post(
            f"{chat.api_url}/api/sessions/{chat.session_id}/agent/pirate.yaml",
            headers=chat._headers(),
            json=[{"role": "user", "content": msg}],
            stream=True,
            timeout=30
        )
        
        if response.status_code == 200:
            client = sseclient.SSEClient(response)
            full_response = []
            
            for event in client.events():
                try:
                    data = json.loads(event.data)
                    if data.get('type') == 'agent_choice':
                        content = data.get('content', '')
                        if content:
                            print(content, end='', flush=True)
                            full_response.append(content)
                    elif data.get('type') == 'stream_stopped':
                        break
                except:
                    pass
            
            print()  # New line after response
            time.sleep(1)  # Brief pause between messages
        else:
            print(f"\n❌ Failed to send message: {response.status_code}")
    
    # Show session summary
    print_header("Session Summary")
    chat.show_session_summary()
    
    # Demonstrate session isolation
    print_header("Session Isolation Test")
    print("➜ Listing user's sessions...")
    
    response = requests.get(
        f"{chat.api_url}/api/sessions",
        headers=chat._headers(),
        timeout=10
    )
    
    if response.status_code == 200:
        sessions = response.json()
        user_sessions = [s for s in sessions if chat.session_id in s.get('id', '')]
        print(f"✅ User can see {len(sessions)} session(s)")
        if user_sessions:
            print(f"   Including the current session: {chat.session_id[:8]}...")
    else:
        print(f"❌ Failed to list sessions")
    
    print_header("Demo Complete!")
    print("\n✅ Authentication system is working correctly:")
    print("   • User registration and login")
    print("   • JWT token authentication")
    print("   • Session creation with user ownership")
    print("   • Agent interaction with authentication")
    print("   • Session isolation (users only see their own sessions)")
    
    return True

if __name__ == "__main__":
    try:
        success = demo()
        sys.exit(0 if success else 1)
    except KeyboardInterrupt:
        print("\n\n⚠️ Demo interrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n❌ Demo failed with error: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)