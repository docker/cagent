#!/usr/bin/env python3
"""
Cagent Interactive Chat Client

Usage: ./cagent_chat.py <agent_name.yaml>
Example: ./cagent_chat.py basic_agent.yaml
"""

import json
import requests
import sys
import argparse
from typing import List, Dict, Optional
import readline  # For better input handling with history

# ANSI color codes for better terminal output
class Colors:
    BLUE = '\033[94m'
    GREEN = '\033[92m'
    YELLOW = '\033[93m'
    RED = '\033[91m'
    CYAN = '\033[96m'
    RESET = '\033[0m'
    BOLD = '\033[1m'
    DIM = '\033[2m'

API_URL = "https://cagent-api-950783879036.us-central1.run.app"

class CagentChatClient:
    def __init__(self, agent_name: str):
        self.agent_name = agent_name.replace('.yaml', '')  # Remove .yaml if present
        self.session_id = None
        self.messages_history = []
        
    def create_session(self) -> bool:
        """Create a new chat session"""
        try:
            response = requests.post(
                f"{API_URL}/api/sessions",
                json={"title": f"Chat with {self.agent_name}"}
            )
            response.raise_for_status()
            session_data = response.json()
            self.session_id = session_data['id']
            return True
        except requests.exceptions.RequestException as e:
            print(f"{Colors.RED}Error creating session: {e}{Colors.RESET}")
            return False
    
    def get_available_agents(self) -> List[Dict]:
        """Get list of available agents"""
        try:
            response = requests.get(f"{API_URL}/api/agents")
            response.raise_for_status()
            return response.json()
        except requests.exceptions.RequestException:
            return []
    
    def verify_agent_exists(self) -> bool:
        """Check if the specified agent exists"""
        agents = self.get_available_agents()
        agent_names = [agent['name'].replace('.yaml', '') for agent in agents]
        return self.agent_name in agent_names or f"{self.agent_name}.yaml" in [agent['name'] for agent in agents]
    
    def send_message(self, message: str) -> Optional[str]:
        """Send a message to the agent and return the response"""
        if not self.session_id:
            print(f"{Colors.RED}No active session{Colors.RESET}")
            return None
        
        try:
            # Add message to history
            self.messages_history.append({"role": "user", "content": message})
            
            # Send request with streaming
            response = requests.post(
                f"{API_URL}/api/sessions/{self.session_id}/agent/{self.agent_name}",
                json=[{"content": message}],
                stream=True
            )
            response.raise_for_status()
            
            # Collect the full response
            full_response = []
            token_usage = None
            
            # Show thinking indicator
            print(f"{Colors.DIM}Thinking...{Colors.RESET}", end='', flush=True)
            
            for chunk in response.iter_lines():
                if chunk:
                    line = chunk.decode('utf-8')
                    if line.startswith('data: '):
                        data_str = line[6:]
                        if data_str and data_str != ' ':
                            try:
                                data = json.loads(data_str)
                                event_type = data.get('type', '')
                                
                                if event_type == 'agent_choice':
                                    # Clear the thinking indicator on first content
                                    if not full_response:
                                        print('\r' + ' ' * 20 + '\r', end='', flush=True)
                                    
                                    content = data.get('content', '')
                                    full_response.append(content)
                                
                                elif event_type == 'token_usage':
                                    token_usage = data.get('usage', {})
                                
                                elif event_type == 'error':
                                    print('\r' + ' ' * 20 + '\r', end='', flush=True)
                                    error_msg = data.get('error', data.get('message', 'Unknown error'))
                                    print(f"{Colors.RED}Error: {error_msg}{Colors.RESET}")
                                    return None
                                    
                            except json.JSONDecodeError:
                                pass
            
            # Clear any remaining thinking indicator
            print('\r' + ' ' * 20 + '\r', end='', flush=True)
            
            # Combine response
            complete_response = ''.join(full_response)
            
            if complete_response:
                # Add response to history
                self.messages_history.append({"role": "assistant", "content": complete_response})
                
                # Show token usage if available
                if token_usage:
                    cost = token_usage.get('cost', 0)
                    if cost > 0:
                        print(f"{Colors.DIM}[Tokens: in={token_usage.get('input_tokens', 0)}, "
                              f"out={token_usage.get('output_tokens', 0)}, "
                              f"cost=${cost:.6f}]{Colors.RESET}")
                
                return complete_response
            else:
                return None
                
        except requests.exceptions.RequestException as e:
            print(f"{Colors.RED}Error sending message: {e}{Colors.RESET}")
            return None
    
    def run_chat(self):
        """Main chat loop"""
        # Verify agent exists
        print(f"{Colors.CYAN}Checking agent availability...{Colors.RESET}")
        if not self.verify_agent_exists():
            print(f"{Colors.RED}Agent '{self.agent_name}' not found!{Colors.RESET}")
            print(f"{Colors.YELLOW}Available agents:{Colors.RESET}")
            for agent in self.get_available_agents():
                print(f"  • {agent['name']}: {agent['description']}")
            return
        
        # Create session
        print(f"{Colors.CYAN}Starting chat session with {self.agent_name}...{Colors.RESET}")
        if not self.create_session():
            print(f"{Colors.RED}Failed to create session{Colors.RESET}")
            return
        
        print(f"{Colors.GREEN}✓ Session created: {self.session_id}{Colors.RESET}")
        print(f"\n{Colors.BOLD}Chat with {self.agent_name}{Colors.RESET}")
        print(f"{Colors.DIM}Type 'quit', 'exit', or press Ctrl+C to end the chat{Colors.RESET}")
        print(f"{Colors.DIM}Type 'clear' to clear the screen{Colors.RESET}")
        print(f"{Colors.DIM}Type 'history' to see message history{Colors.RESET}")
        print("-" * 60)
        
        try:
            while True:
                # Get user input
                try:
                    user_input = input(f"\n{Colors.BOLD}{Colors.BLUE}You:{Colors.RESET} ")
                except EOFError:
                    break
                
                # Check for commands
                if user_input.lower() in ['quit', 'exit', 'q']:
                    break
                elif user_input.lower() == 'clear':
                    print("\033[2J\033[H")  # Clear screen
                    print(f"{Colors.BOLD}Chat with {self.agent_name}{Colors.RESET}")
                    print("-" * 60)
                    continue
                elif user_input.lower() == 'history':
                    self.show_history()
                    continue
                elif not user_input.strip():
                    continue
                
                # Send message and get response
                response = self.send_message(user_input)
                
                if response:
                    # Display response
                    print(f"\n{Colors.BOLD}{Colors.GREEN}{self.agent_name}:{Colors.RESET} {response}")
                else:
                    print(f"{Colors.YELLOW}No response received{Colors.RESET}")
        
        except KeyboardInterrupt:
            pass
        
        print(f"\n{Colors.CYAN}Chat session ended{Colors.RESET}")
        self.show_summary()
    
    def show_history(self):
        """Display chat history"""
        print(f"\n{Colors.BOLD}Chat History:{Colors.RESET}")
        print("-" * 60)
        for msg in self.messages_history:
            if msg['role'] == 'user':
                print(f"{Colors.BLUE}You:{Colors.RESET} {msg['content']}")
            else:
                print(f"{Colors.GREEN}{self.agent_name}:{Colors.RESET} {msg['content']}")
        print("-" * 60)
    
    def show_summary(self):
        """Show session summary"""
        if self.session_id:
            try:
                response = requests.get(f"{API_URL}/api/sessions/{self.session_id}")
                if response.status_code == 200:
                    info = response.json()
                    print(f"\n{Colors.BOLD}Session Summary:{Colors.RESET}")
                    print(f"  • Messages exchanged: {len(self.messages_history)}")
                    print(f"  • Input tokens: {info.get('input_tokens', 0)}")
                    print(f"  • Output tokens: {info.get('output_tokens', 0)}")
                    if info.get('title'):
                        print(f"  • Title: {info['title']}")
            except:
                pass

def main():
    parser = argparse.ArgumentParser(
        description='Interactive chat client for cagent API',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s basic_agent.yaml     - Chat with basic_agent
  %(prog)s haiku                 - Chat with haiku agent
  %(prog)s pirate.yaml          - Chat with pirate agent
  
Available commands during chat:
  quit/exit/q  - End the chat session
  clear        - Clear the screen
  history      - Show message history
        """
    )
    parser.add_argument(
        'agent',
        nargs='?',  # Make agent optional
        help='Name of the agent to chat with (with or without .yaml extension)'
    )
    parser.add_argument(
        '--list',
        action='store_true',
        help='List available agents'
    )
    parser.add_argument(
        '--no-color',
        action='store_true',
        help='Disable colored output'
    )
    
    args = parser.parse_args()
    
    # Disable colors if requested
    if args.no_color:
        for attr in dir(Colors):
            if not attr.startswith('_'):
                setattr(Colors, attr, '')
    
    # List agents if requested
    if args.list or not args.agent:
        print(f"{Colors.BOLD}Available agents on cagent API:{Colors.RESET}")
        try:
            response = requests.get(f"{API_URL}/api/agents")
            response.raise_for_status()
            agents = response.json()
            for agent in agents:
                print(f"  • {Colors.CYAN}{agent['name']}{Colors.RESET}: {agent['description']}")
            print(f"\n{Colors.DIM}Usage: {sys.argv[0]} <agent_name>{Colors.RESET}")
        except requests.exceptions.RequestException as e:
            print(f"{Colors.RED}Error fetching agents: {e}{Colors.RESET}")
        return
    
    # Create and run chat client
    client = CagentChatClient(args.agent)
    client.run_chat()

if __name__ == "__main__":
    main()
