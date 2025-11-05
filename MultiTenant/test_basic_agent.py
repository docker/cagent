#!/usr/bin/env python3
"""
Test script for cagent API with basic_agent
"""

import json
import requests
import sys

API_URL = "https://YOUR-API-URL"

def main():
    print("🚀 Testing cagent API with basic_agent...")
    
    # Create session
    print("\n1. Creating a new session...")
    session_response = requests.post(
        f"{API_URL}/api/sessions",
        json={"title": "Basic Agent Test"}
    )
    session_data = session_response.json()
    session_id = session_data['id']
    print(f"✅ Session created: {session_id}")
    
    # Send message to basic_agent
    print("\n2. Sending a simple question to basic_agent...")
    
    agent_response = requests.post(
        f"{API_URL}/api/sessions/{session_id}/agent/basic_agent",
        json=[{"content": "What is 2 + 2? Please answer in one sentence."}],
        stream=True
    )
    
    print("\n--- Response Stream ---\n")
    
    full_content = []
    got_content = False
    
    # Process the streaming response
    for chunk in agent_response.iter_lines():
        if chunk:
            line = chunk.decode('utf-8')
            if line.startswith('data: '):
                data_str = line[6:]
                if data_str and data_str != ' ':
                    try:
                        data = json.loads(data_str)
                        event_type = data.get('type', '')
                        
                        if event_type == 'user_message':
                            print(f"📨 User: {data.get('message', '')}")
                        elif event_type == 'stream_started':
                            print(f"🔧 Stream started for agent: {data.get('agent_name')}")
                        elif event_type == 'agent_choice':
                            content = data.get('content', '')
                            full_content.append(content)
                            got_content = True
                            print(f"🤖 Agent response: {content}")
                        elif event_type == 'token_usage':
                            usage = data.get('usage', {})
                            print(f"📊 Tokens - Input: {usage.get('input_tokens')}, Output: {usage.get('output_tokens')}, Cost: ${usage.get('cost', 0):.6f}")
                        elif event_type == 'stream_stopped':
                            print(f"✅ Stream completed")
                        elif event_type == 'session_title':
                            print(f"📝 Session titled: {data.get('title', '')}")
                        elif event_type == 'error':
                            error_msg = data.get('error', data.get('message', 'Unknown error'))
                            print(f"\n❌ Error: {error_msg}")
                            break
                        
                    except json.JSONDecodeError as e:
                        pass  # Ignore parse errors
    
    print("\n--- End of Stream ---")
    
    if full_content:
        print("\n📋 Complete response:")
        print("=" * 40)
        print(''.join(full_content))
        print("=" * 40)
    
    # Get session info
    print(f"\n3. Getting session info...")
    session_info = requests.get(f"{API_URL}/api/sessions/{session_id}")
    if session_info.status_code == 200:
        info = session_info.json()
        print(f"   Session: {info.get('id', 'N/A')}")
        print(f"   Title: {info.get('title', 'N/A')}")
        print(f"   Messages: {info.get('num_messages', 0)}")
        print(f"   Input tokens: {info.get('input_tokens', 0)}")
        print(f"   Output tokens: {info.get('output_tokens', 0)}")

if __name__ == "__main__":
    main()