#!/usr/bin/env python3
"""
Test script for cagent API with proper SSE streaming support
"""

import json
import requests
import sys
from typing import Iterator

API_URL = "https://YOUR-API-URL"

def parse_sse_stream(response_text: str) -> Iterator[dict]:
    """Parse SSE stream data"""
    for line in response_text.split('\n'):
        if line.startswith('data: '):
            data_str = line[6:]  # Remove 'data: ' prefix
            if data_str and data_str != ' ':
                try:
                    yield json.loads(data_str)
                except json.JSONDecodeError:
                    continue

def main():
    print("🚀 Creating a new session...")
    
    # Create session
    session_response = requests.post(
        f"{API_URL}/api/sessions",
        json={"title": "Python Haiku Test"}
    )
    session_data = session_response.json()
    session_id = session_data['id']
    print(f"✅ Session created: {session_id}")
    
    # Send message to agent
    print("\n📮 Sending request to generate a haiku about the cloud...")
    
    agent_response = requests.post(
        f"{API_URL}/api/sessions/{session_id}/agent/haiku",
        json=[{"content": "Write a haiku about cloud computing"}],
        stream=True
    )
    
    print("\n--- Streaming Response ---\n")
    
    full_content = []
    
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
                        
                        if event_type == 'mcp_init_started':
                            print(f"🔧 MCP init started for agent: {data.get('agent_name')}")
                        elif event_type == 'mcp_init_completed':
                            print(f"✅ MCP init completed")
                        elif event_type == 'message_start':
                            print("📝 Message generation started")
                        elif event_type == 'content_block_start':
                            pass  # Silent
                        elif event_type == 'content_block_delta':
                            if 'delta' in data and 'text' in data['delta']:
                                content = data['delta']['text']
                                full_content.append(content)
                                sys.stdout.write(content)
                                sys.stdout.flush()
                        elif event_type == 'content_block_stop':
                            pass  # Silent
                        elif event_type == 'message_delta':
                            if 'usage' in data:
                                pass  # Could print usage stats here
                        elif event_type == 'message_stop':
                            print("\n\n✅ Message generation completed")
                        elif event_type == 'error':
                            error_msg = data.get('error', data.get('message', 'Unknown error'))
                            print(f"\n❌ Error: {error_msg}")
                        
                    except json.JSONDecodeError as e:
                        print(f"⚠️ Failed to parse JSON: {e}")
    
    print("\n--- End of Stream ---")
    
    if full_content:
        print("\n📋 Complete haiku:")
        print("=" * 40)
        print(''.join(full_content))
        print("=" * 40)
    
    # Get session info to see token usage
    print(f"\n📊 Getting session info...")
    session_info = requests.get(f"{API_URL}/api/sessions/{session_id}")
    if session_info.status_code == 200:
        info = session_info.json()
        print(f"   Input tokens: {info.get('input_tokens', 0)}")
        print(f"   Output tokens: {info.get('output_tokens', 0)}")
        print(f"   Messages: {info.get('num_messages', 0)}")

if __name__ == "__main__":
    main()