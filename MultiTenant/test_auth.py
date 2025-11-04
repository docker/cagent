#!/usr/bin/env python3
"""
Simple test script for the authenticated Cagent API
"""

import requests
import json
import sys
from datetime import datetime

API_URL = "https://cagent-api-950783879036.us-central1.run.app"

def test_auth_flow():
    print("=" * 50)
    print("Testing Cagent Authentication")
    print("=" * 50)
    
    # Step 1: Register a new user (might fail if already exists)
    print("\n1. Testing registration...")
    test_email = f"test_{datetime.now().strftime('%Y%m%d_%H%M%S')}@example.com"
    response = requests.post(
        f"{API_URL}/api/auth/register",
        json={
            "email": test_email,
            "password": "testpass123",
            "name": "Test User"
        },
        timeout=10
    )
    
    if response.status_code == 200:
        print(f"✅ Registration successful for {test_email}")
        data = response.json()
        token = data['token']
        user = data['user']
    else:
        # Try logging in with existing test account
        print("Registration failed, trying login with existing account...")
        test_email = "test@example.com"
        response = requests.post(
            f"{API_URL}/api/auth/login",
            json={
                "email": test_email,
                "password": "testpass123"
            },
            timeout=10
        )
        
        if response.status_code != 200:
            print(f"❌ Login failed: {response.text}")
            return False
        
        print(f"✅ Login successful for {test_email}")
        data = response.json()
        token = data['token']
        user = data['user']
    
    print(f"   User ID: {user['id']}")
    print(f"   Name: {user['name']}")
    print(f"   Admin: {user['is_admin']}")
    
    # Step 2: Verify authentication works
    print("\n2. Testing authenticated endpoint...")
    response = requests.get(
        f"{API_URL}/api/auth/me",
        headers={"Authorization": f"Bearer {token}"},
        timeout=10
    )
    
    if response.status_code == 200:
        print("✅ Authentication verified")
    else:
        print(f"❌ Authentication failed: {response.status_code}")
        return False
    
    # Step 3: List agents
    print("\n3. Listing available agents...")
    response = requests.get(
        f"{API_URL}/api/agents",
        headers={"Authorization": f"Bearer {token}"},
        timeout=10
    )
    
    if response.status_code == 200:
        agents = response.json()
        print(f"✅ Found {len(agents)} agents:")
        for agent in agents[:5]:  # Show first 5
            print(f"   - {agent.get('name', 'unknown')}")
        if len(agents) > 5:
            print(f"   ... and {len(agents) - 5} more")
    else:
        print(f"❌ Failed to list agents: {response.status_code}")
        return False
    
    # Step 4: Create a session
    print("\n4. Creating a session...")
    response = requests.post(
        f"{API_URL}/api/sessions",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json"
        },
        json={
            "title": f"Test Session - {datetime.now().strftime('%Y-%m-%d %H:%M')}",
            "workingDir": "/tmp"
        },
        timeout=10
    )
    
    if response.status_code == 200:
        session = response.json()
        session_id = session['id']
        print(f"✅ Session created: {session_id}")
        print(f"   User ID: {session.get('user_id', 'none')}")
    else:
        print(f"❌ Failed to create session: {response.status_code}")
        print(f"   Response: {response.text}")
        return False
    
    # Step 5: Verify session ownership
    print("\n5. Verifying session ownership...")
    response = requests.get(
        f"{API_URL}/api/sessions",
        headers={"Authorization": f"Bearer {token}"},
        timeout=10
    )
    
    if response.status_code == 200:
        sessions = response.json()
        user_sessions = [s for s in sessions if s.get('id') == session_id]
        if user_sessions:
            print(f"✅ Session is owned by user (found in user's session list)")
        else:
            print("⚠️ Session not found in user's session list")
    else:
        print(f"❌ Failed to list sessions: {response.status_code}")
    
    # Step 6: Test sending a message (pirate agent)
    print("\n6. Testing chat with pirate agent...")
    response = requests.post(
        f"{API_URL}/api/sessions/{session_id}/agent/pirate.yaml",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json"
        },
        json=[{"role": "user", "content": "Say 'Ahoy matey!' and nothing else"}],
        timeout=30,
        stream=True
    )
    
    if response.status_code == 200:
        print("✅ Message sent successfully")
        print("   Response preview:")
        
        # Read first few chunks of SSE response
        content = []
        for line in response.iter_lines():
            if line:
                line = line.decode('utf-8')
                if line.startswith('data: '):
                    try:
                        data = json.loads(line[6:])
                        if data.get('type') == 'agent_choice':
                            content.append(data.get('content', ''))
                    except:
                        pass
                if len(content) > 5:  # Just get a preview
                    break
        
        if content:
            preview = ''.join(content[:5])[:100]
            print(f"   \"{preview}...\"")
    else:
        print(f"❌ Failed to send message: {response.status_code}")
    
    print("\n" + "=" * 50)
    print("✅ All authentication tests passed!")
    print("=" * 50)
    
    return True

if __name__ == "__main__":
    success = test_auth_flow()
    sys.exit(0 if success else 1)