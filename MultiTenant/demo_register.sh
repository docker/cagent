#!/bin/bash

echo "=================================================="
echo " Cagent Authentication Demo - User Registration"
echo "=================================================="
echo ""
echo "This demo shows how to register a new user using AuthAgentChat.py"
echo ""

# Clear any existing credentials for a clean demo
echo "1. Clearing any existing credentials..."
rm -f ~/.cagent_auth.json
echo "   ✓ Credentials cleared"
echo ""

# Show the registration command
echo "2. Running registration command:"
echo "   $ python3 AuthAgentChat.py --register"
echo ""
echo "   You will be prompted for:"
echo "   - Email address"
echo "   - Your name"
echo "   - Password (min 8 characters)"
echo "   - Password confirmation"
echo ""

read -p "Press Enter to continue with registration..."
echo ""

# Run the actual registration
python3 AuthAgentChat.py --register

echo ""
echo "=================================================="
echo " Registration Complete!"
echo "=================================================="
echo ""
echo "Your credentials have been saved locally in ~/.cagent_auth.json"
echo ""
echo "You can now:"
echo "1. Chat with any agent:"
echo "   $ python3 AuthAgentChat.py pirate.yaml"
echo ""
echo "2. Register another user:"
echo "   $ python3 AuthAgentChat.py --register"
echo ""
echo "3. Logout (clear credentials):"
echo "   $ python3 AuthAgentChat.py --logout"
echo ""
echo "4. The client will auto-detect if auth is required"
echo "   and use your saved credentials automatically!"
echo ""