#!/usr/bin/env python3
"""
AuthAgentChat - Enhanced Cagent Chat Client with Authentication Support

This client supports both authenticated and non-authenticated modes for the Cagent API.
It handles user registration, login, and token management automatically.
"""

import argparse
import json
import os
import sys
import requests
from typing import Optional, Dict, Any
from datetime import datetime
import getpass

# Try to import sseclient, suggest installation if missing
try:
    import sseclient
except ImportError:
    print("Error: sseclient is not installed.")
    print("Please install it with: pip install sseclient-py")
    sys.exit(1)

class AuthAgentChat:
    def __init__(self, api_url: str, agent_yaml: str, verbose: bool = False, timeout: int = 120):
        """
        Initialize the authenticated agent chat client.
        
        Args:
            api_url: The base URL of the Cagent API
            agent_yaml: The name or path of the agent YAML file
            verbose: Whether to show verbose output
            timeout: Request timeout in seconds
        """
        self.api_url = api_url.rstrip('/')
        self.agent_yaml = agent_yaml
        self.verbose = verbose
        self.timeout = timeout
        self.session_id = None
        self.token = None
        self.user = None
        self.auth_enabled = self._check_auth_enabled()
        
        # Load saved credentials if they exist
        self.creds_file = os.path.expanduser("~/.cagent_auth.json")
        self._load_credentials()
        
    def _check_auth_enabled(self) -> bool:
        """Check if authentication is enabled on the server."""
        try:
            response = requests.get(f"{self.api_url}/api/sessions", timeout=5)
            if response.status_code == 401 or response.status_code == 403:
                if self.verbose:
                    print("Authentication is enabled on the server")
                return True
            elif response.status_code == 200:
                if self.verbose:
                    print("Authentication is disabled on the server")
                return False
            else:
                if self.verbose:
                    print(f"Unexpected response: {response.status_code}")
                return False
        except Exception as e:
            if self.verbose:
                print(f"Error checking auth status: {e}")
            return False
    
    def _load_credentials(self):
        """Load saved credentials from file."""
        if os.path.exists(self.creds_file):
            try:
                with open(self.creds_file, 'r') as f:
                    data = json.load(f)
                    if self.api_url in data:
                        creds = data[self.api_url]
                        self.token = creds.get('token')
                        self.user = creds.get('user')
                        if self.verbose and self.token:
                            print(f"Loaded saved credentials for {self.user.get('email', 'unknown')}")
            except Exception as e:
                if self.verbose:
                    print(f"Could not load saved credentials: {e}")
    
    def _save_credentials(self):
        """Save credentials to file."""
        try:
            data = {}
            if os.path.exists(self.creds_file):
                with open(self.creds_file, 'r') as f:
                    data = json.load(f)
            
            data[self.api_url] = {
                'token': self.token,
                'user': self.user,
                'saved_at': datetime.now().isoformat()
            }
            
            with open(self.creds_file, 'w') as f:
                json.dump(data, f, indent=2)
            
            # Set file permissions to be readable only by the user
            os.chmod(self.creds_file, 0o600)
            
            if self.verbose:
                print(f"Saved credentials to {self.creds_file}")
        except Exception as e:
            if self.verbose:
                print(f"Could not save credentials: {e}")
    
    def _clear_credentials(self):
        """Clear saved credentials."""
        self.token = None
        self.user = None
        if os.path.exists(self.creds_file):
            try:
                with open(self.creds_file, 'r') as f:
                    data = json.load(f)
                if self.api_url in data:
                    del data[self.api_url]
                with open(self.creds_file, 'w') as f:
                    json.dump(data, f, indent=2)
            except:
                pass
    
    def _headers(self) -> Dict[str, str]:
        """Get headers with authentication if available."""
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"
        return headers
    
    def register(self) -> bool:
        """Register a new user."""
        print("\n=== User Registration ===")
        email = input("Email: ").strip()
        name = input("Name: ").strip()
        password = getpass.getpass("Password: ")
        confirm = getpass.getpass("Confirm Password: ")
        
        if password != confirm:
            print("❌ Passwords do not match")
            return False
        
        if len(password) < 8:
            print("❌ Password must be at least 8 characters")
            return False
        
        try:
            response = requests.post(
                f"{self.api_url}/api/auth/register",
                json={
                    "email": email,
                    "password": password,
                    "name": name
                },
                timeout=10
            )
            
            if response.status_code == 200:
                data = response.json()
                self.token = data.get('token')
                self.user = data.get('user')
                self._save_credentials()
                print(f"✅ Registration successful! Welcome, {name}")
                return True
            else:
                error = response.json().get('message', 'Registration failed')
                print(f"❌ Registration failed: {error}")
                return False
                
        except Exception as e:
            print(f"❌ Registration error: {e}")
            return False
    
    def login(self) -> bool:
        """Login with existing credentials."""
        print("\n=== User Login ===")
        email = input("Email: ").strip()
        password = getpass.getpass("Password: ")
        
        try:
            response = requests.post(
                f"{self.api_url}/api/auth/login",
                json={
                    "email": email,
                    "password": password
                },
                timeout=10
            )
            
            if response.status_code == 200:
                data = response.json()
                self.token = data.get('token')
                self.user = data.get('user')
                self._save_credentials()
                print(f"✅ Login successful! Welcome back, {self.user.get('name', email)}")
                return True
            else:
                error = response.json().get('message', 'Login failed')
                print(f"❌ Login failed: {error}")
                return False
                
        except Exception as e:
            print(f"❌ Login error: {e}")
            return False
    
    def authenticate(self) -> bool:
        """Handle authentication - login or register."""
        if not self.auth_enabled:
            return True  # No auth needed
        
        # Try to validate existing token
        if self.token:
            try:
                response = requests.get(
                    f"{self.api_url}/api/auth/me",
                    headers=self._headers(),
                    timeout=5
                )
                if response.status_code == 200:
                    self.user = response.json()
                    print(f"✅ Authenticated as {self.user.get('email')}")
                    return True
                else:
                    print("⚠️ Saved token is invalid, please login again")
                    self._clear_credentials()
            except:
                pass
        
        # Ask user to login or register
        print("\n" + "="*50)
        print("Authentication Required")
        print("="*50)
        
        while True:
            choice = input("\n1. Login\n2. Register new account\n3. Exit\n\nChoice (1-3): ").strip()
            
            if choice == '1':
                if self.login():
                    return True
            elif choice == '2':
                if self.register():
                    return True
            elif choice == '3':
                return False
            else:
                print("Invalid choice. Please enter 1, 2, or 3.")
    
    def verify_agent(self) -> bool:
        """Verify that the specified agent exists."""
        try:
            response = requests.get(
                f"{self.api_url}/api/agents",
                headers=self._headers(),
                timeout=10
            )
            response.raise_for_status()
            agents = response.json()
            
            # Check if agent exists (with or without .yaml extension)
            agent_names = []
            for agent in agents:
                if isinstance(agent, dict):
                    # Handle both 'path' and 'name' fields
                    name = agent.get('name') or agent.get('path', '')
                    if name:
                        agent_names.append(name)
                else:
                    agent_names.append(agent)
            
            agent_check = self.agent_yaml
            if not agent_check.endswith('.yaml'):
                agent_check += '.yaml'
            
            if agent_check not in agent_names:
                print(f"\n❌ Agent '{self.agent_yaml}' not found!")
                print("\nAvailable agents:")
                for agent in agent_names:
                    print(f"  - {agent}")
                return False
            
            print(f"✅ Agent '{self.agent_yaml}' verified")
            return True
            
        except Exception as e:
            print(f"❌ Error verifying agent: {e}")
            return False
    
    def create_session(self, title: str = None, tools_approved: bool = True) -> bool:
        """Create a new chat session.
        
        Args:
            title: Optional session title
            tools_approved: Whether to auto-approve tool usage (default: True for API mode)
        """
        if not title:
            title = f"Chat with {self.agent_yaml} - {datetime.now().strftime('%Y-%m-%d %H:%M')}"
        
        try:
            response = requests.post(
                f"{self.api_url}/api/sessions",
                headers=self._headers(),
                json={
                    "title": title,
                    "workingDir": "/tmp",
                    "tools_approved": tools_approved  # Enable auto-approval of tools
                },
                timeout=10
            )
            response.raise_for_status()
            
            session = response.json()
            self.session_id = session['id']
            
            print(f"\n✅ Session created: {self.session_id}")
            if self.verbose:
                print(f"   Title: {title}")
            
            return True
            
        except Exception as e:
            print(f"❌ Error creating session: {e}")
            return False
    
    def send_message(self, message: str, show_progress: bool = True):
        """Send a message to the agent and stream the response."""
        if not self.session_id:
            print("❌ No active session")
            return
        
        # Prepare the message payload
        payload = [{"role": "user", "content": message}]
        
        # Retry logic for timeout errors
        max_retries = 3
        retry_count = 0
        timeout_seconds = self.timeout
        
        while retry_count < max_retries:
            try:
                # Send the message
                response = requests.post(
                    f"{self.api_url}/api/sessions/{self.session_id}/agent/{self.agent_yaml}",
                    headers=self._headers(),
                    json=payload,
                    stream=True,
                    timeout=timeout_seconds
                )
                response.raise_for_status()
                break  # Success, exit retry loop
            except requests.exceptions.Timeout:
                retry_count += 1
                if retry_count < max_retries:
                    print(f"\n⏱️ Request timed out. Retrying... ({retry_count}/{max_retries})")
                    continue
                else:
                    print(f"\n❌ Request timed out after {max_retries} attempts. The agent might be processing a complex request.")
                    print("Try again with a simpler query or increase timeout with --timeout flag.")
                    return
            except requests.exceptions.HTTPError as e:
                if e.response.status_code == 401:
                    print("❌ Authentication expired. Please restart the chat.")
                    self._clear_credentials()
                else:
                    print(f"❌ HTTP Error: {e}")
                return
            except Exception as e:
                print(f"❌ Error sending message: {e}")
                return
        
        # Stream the response
        if show_progress:
            print("\n" + "="*50)
            print("Agent Response:")
            print("="*50 + "\n")
        
        client = sseclient.SSEClient(response)
        full_response = []
        token_usage = {}
        
        for event in client.events():
            try:
                data = json.loads(event.data)
                event_type = data.get('type', '')
                
                if event_type == 'agent_choice':
                    content = data.get('content', '')
                    if content:
                        print(content, end='', flush=True)
                        full_response.append(content)
                
                elif event_type == 'token_usage':
                    token_usage = data.get('usage', {})
                
                elif event_type == 'error':
                    print(f"\n❌ Error: {data.get('message', 'Unknown error')}")
                    break
                
                # Show progress events for better UX
                elif event_type == 'tool_call' and show_progress:
                    tool_call = data.get('tool_call', {})
                    tool_name = tool_call.get('name', 'unknown')
                    print(f"\n🔧 [Calling tool: {tool_name}]", flush=True)
                    if self.verbose:
                        args = tool_call.get('arguments', {})
                        if args:
                            print(f"   Arguments: {json.dumps(args, indent=2)}")
                
                elif event_type == 'partial_tool_call' and show_progress and self.verbose:
                    tool_call = data.get('tool_call', {})
                    tool_name = tool_call.get('name', 'unknown')
                    print(f"\n⚙️ [Preparing tool: {tool_name}]", flush=True)
                
                elif event_type == 'tool_call_response' and show_progress:
                    tool_call = data.get('tool_call', {})
                    tool_name = tool_call.get('name', 'unknown')
                    print(f"\n✓ [Tool {tool_name} completed]", flush=True)
                
                elif event_type == 'agent_choice_reasoning' and show_progress:
                    content = data.get('content', '')
                    if content:
                        print(f"\n💭 [Reasoning: {content[:100]}...]", flush=True)
                
                elif event_type == 'warning' and show_progress:
                    warning_msg = data.get('message', '')
                    if warning_msg:
                        print(f"\n⚠️ Warning: {warning_msg}", flush=True)
                
                elif event_type == 'shell' and show_progress:
                    output = data.get('output', '')
                    if output and self.verbose:
                        print(f"\n💻 Shell output:\n{output[:200]}...", flush=True)
                
                elif self.verbose:
                    # Show other events in verbose mode
                    if event_type not in ['stream_started', 'stream_stopped', 'user_message']:
                        # Don't duplicate events we've already handled
                        if event_type not in ['tool_call', 'partial_tool_call', 'tool_call_response', 
                                               'agent_choice_reasoning', 'warning', 'shell']:
                            print(f"\n[{event_type}]: {json.dumps(data, indent=2)}")
            
            except json.JSONDecodeError:
                if self.verbose:
                    print(f"\nRaw event: {event.data}")
            except Exception as e:
                if self.verbose:
                    print(f"\nError processing event: {e}")
        
        print("\n")  # End the response with newlines
        
        # Show token usage if available
        if token_usage and show_progress:
            print("\n" + "-"*50)
            print(f"Tokens - Input: {token_usage.get('input_tokens', 0)}, "
                  f"Output: {token_usage.get('output_tokens', 0)}, "
                  f"Total: {token_usage.get('total_tokens', 0)}")
    
    def get_session_history(self):
        """Retrieve and display session history."""
        if not self.session_id:
            print("No active session")
            return
        
        try:
            response = requests.get(
                f"{self.api_url}/api/sessions/{self.session_id}",
                headers=self._headers(),
                timeout=10
            )
            response.raise_for_status()
            
            session = response.json()
            messages = session.get('messages', [])
            
            if messages:
                print("\n" + "="*50)
                print("Session History:")
                print("="*50)
                for msg in messages:
                    role = msg.get('role', 'unknown')
                    content = msg.get('content', '')
                    print(f"\n[{role.upper()}]: {content[:200]}...")
            else:
                print("\nNo messages in history yet.")
                
        except Exception as e:
            print(f"Error retrieving history: {e}")
    
    def show_session_summary(self):
        """Display a summary of the current session."""
        if not self.session_id:
            return
        
        try:
            response = requests.get(
                f"{self.api_url}/api/sessions/{self.session_id}",
                headers=self._headers(),
                timeout=10
            )
            response.raise_for_status()
            
            session = response.json()
            
            print("\n" + "="*50)
            print("Session Summary:")
            print("="*50)
            print(f"ID: {session.get('id', 'N/A')}")
            print(f"Title: {session.get('title', 'N/A')}")
            print(f"Created: {session.get('created_at', 'N/A')}")
            print(f"Messages: {len(session.get('messages', []))}")
            print(f"Input Tokens: {session.get('input_tokens', 0)}")
            print(f"Output Tokens: {session.get('output_tokens', 0)}")
            
        except Exception as e:
            if self.verbose:
                print(f"Could not retrieve session summary: {e}")
    
    def interactive_chat(self, initial_prompt: str = None):
        """Run an interactive chat session."""
        print("\n" + "="*50)
        print(f"Chat with {self.agent_yaml}")
        if self.auth_enabled and self.user:
            print(f"User: {self.user.get('email', 'Unknown')}")
        print("="*50)
        print("\nCommands:")
        print("  /exit    - Exit the chat")
        print("  /history - Show session history")
        print("  /summary - Show session summary")
        print("  /logout  - Logout and clear credentials")
        print("\n" + "="*50)
        
        # Send initial prompt if provided
        if initial_prompt:
            print(f"\n🧑 You: {initial_prompt}")
            print("\n🤖 Agent: ", end='')
            self.send_message(initial_prompt)
            print("\n" + "="*50)
        
        while True:
            try:
                # Get user input
                user_input = input("\n🧑 You: ").strip()
                
                if not user_input:
                    continue
                
                # Handle commands
                if user_input.lower() == '/exit':
                    print("\n👋 Goodbye!")
                    break
                elif user_input.lower() == '/history':
                    self.get_session_history()
                    continue
                elif user_input.lower() == '/summary':
                    self.show_session_summary()
                    continue
                elif user_input.lower() == '/logout':
                    self._clear_credentials()
                    print("✅ Logged out and credentials cleared")
                    break
                
                # Send message to agent
                print("\n🤖 Agent: ", end='')
                self.send_message(user_input)
                
            except KeyboardInterrupt:
                print("\n\n👋 Chat interrupted. Goodbye!")
                break
            except Exception as e:
                print(f"\n❌ Error: {e}")
                if self.verbose:
                    import traceback
                    traceback.print_exc()

def main():
    """Main entry point for the chat client."""
    parser = argparse.ArgumentParser(
        description='Chat with a Cagent API agent (with authentication support)',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s pirate.yaml                                    # Chat with pirate agent using default API
  %(prog)s basic_agent --url http://localhost:8080        # Use local API
  %(prog)s pirate.yaml --verbose                          # Show detailed output
  %(prog)s gmail_agent.yaml "Check my latest emails"      # Send initial prompt
  %(prog)s pirate.yaml --timeout 300                      # Use 5 minute timeout
  %(prog)s pirate.yaml --no-auth                          # Skip authentication even if enabled
  %(prog)s --register                                     # Register a new user account
  %(prog)s --logout                                        # Clear saved credentials
  

The client will automatically detect if authentication is required and prompt for login/registration.
Credentials are saved locally for future use.
        """
    )
    
    parser.add_argument(
        'agent',
        nargs='?',  # Make agent optional
        help='Name of the agent YAML file (e.g., pirate.yaml or just pirate)'
    )
    parser.add_argument(
        'prompt',
        nargs='?',
        help='Optional initial prompt to send to the agent'
    )
    parser.add_argument(
        '--url',
        default='https://cagent-api-950783879036.us-central1.run.app',
        help='Cagent API URL (default: Cloud Run deployment)'
    )
    
    parser.add_argument(
        '--verbose', '-v',
        action='store_true',
        help='Show verbose output including all SSE events'
    )
    
    parser.add_argument(
        '--timeout', '-t',
        type=int,
        default=120,
        help='Request timeout in seconds (default: 120)'
    )
    
    parser.add_argument(
        '--no-auth',
        action='store_true',
        help='Skip authentication (only works if server has auth disabled)'
    )
    
    parser.add_argument(
        '--logout',
        action='store_true',
        help='Clear saved credentials and exit'
    )
    
    parser.add_argument(
        '--register',
        action='store_true',
        help='Register a new user account'
    )
    
    args = parser.parse_args()
    
    # Clear credentials if requested
    if args.logout:
        creds_file = os.path.expanduser("~/.cagent_auth.json")
        if os.path.exists(creds_file):
            os.remove(creds_file)
            print("✅ Credentials cleared")
        else:
            print("No saved credentials found")
        return
    
    # Handle registration command
    if args.register:
        print("\n" + "="*50)
        print("Cagent User Registration")
        print("="*50)
        
        # Create a minimal chat client just for registration
        chat = AuthAgentChat(args.url, "dummy", args.verbose, args.timeout)
        
        if not chat.auth_enabled:
            print("\n⚠️ Authentication is disabled on this server.")
            print("Registration is not needed - you can use the API directly.")
            return
        
        # Perform registration
        if chat.register():
            print("\n" + "="*50)
            print("Registration Complete!")
            print("="*50)
            print(f"\n✅ You are now registered and logged in as {chat.user.get('email')}")
            print(f"   User ID: {chat.user.get('id')}")
            print(f"   Name: {chat.user.get('name')}")
            print("\nYour credentials have been saved locally.")
            print("You can now use any of the chat commands to interact with agents.")
            print("\nExample:")
            print(f"  python3 {sys.argv[0]} pirate.yaml")
        else:
            print("\n❌ Registration failed. Please try again.")
            sys.exit(1)
        return
    
    # Check if agent was provided (required for chat mode)
    if not args.agent:
        parser.error("agent is required unless using --register or --logout")
    
    # Ensure agent has .yaml extension
    agent_yaml = args.agent
    if not agent_yaml.endswith('.yaml') and not agent_yaml.endswith('.yml'):
        agent_yaml += '.yaml'
    
    # Create chat client
    chat = AuthAgentChat(args.url, agent_yaml, args.verbose, args.timeout)
    
    # Handle authentication if needed
    if not args.no_auth:
        if chat.auth_enabled:
            if not chat.authenticate():
                print("❌ Authentication failed. Exiting.")
                return
        else:
            print("ℹ️ Authentication is not required for this server")
    
    # Verify agent exists
    if not chat.verify_agent():
        return
    
    # Create session
    if not chat.create_session():
        return
    
    try:
        # Start interactive chat
        chat.interactive_chat(args.prompt)
    finally:
        # Show final summary
        chat.show_session_summary()

if __name__ == '__main__':
    main()
