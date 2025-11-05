#!/bin/bash

# Cagent Cloud Run API Test Script
API_URL="https://YOUR-API-URL"

echo "🚀 Testing Cagent API at $API_URL"
echo "=" 

# 1. Test API Health
echo "1. Testing API Health..."
curl -s "$API_URL/api/ping" | jq '.'
echo ""

# 2. List Available Agents
echo "2. Listing Available Agents..."
curl -s "$API_URL/api/agents" | jq '.'
echo ""

# 3. Create a new session
echo "3. Creating a new session..."
SESSION_RESPONSE=$(curl -s -X POST "$API_URL/api/sessions" \
  -H "Content-Type: application/json" \
  -d '{"title": "Test Session", "workingDir": "/work"}')
echo "$SESSION_RESPONSE" | jq '.'
SESSION_ID=$(echo "$SESSION_RESPONSE" | jq -r '.id')
echo "Session ID: $SESSION_ID"
echo ""

# 4. Test the pirate agent with a simple question
echo "4. Testing the pirate agent..."
echo "Sending message: 'Hello, what is your favorite treasure?'"
echo ""
curl -X POST "$API_URL/api/sessions/$SESSION_ID/agent/pirate" \
  -H "Content-Type: application/json" \
  -d '[{"content": "Hello, what is your favorite treasure?"}]' \
  2>/dev/null | head -20
echo ""
echo "..."
echo ""

# 5. Quick test with code agent
echo "5. Quick test with code agent..."
echo "Creating new session for code agent..."
CODE_SESSION=$(curl -s -X POST "$API_URL/api/sessions" \
  -H "Content-Type: application/json" \
  -d '{"title": "Code Session", "workingDir": "/work"}' | jq -r '.id')
echo "Session ID: $CODE_SESSION"
echo ""
echo "Asking: 'Write a hello world function in Python'"
curl -X POST "$API_URL/api/sessions/$CODE_SESSION/agent/code" \
  -H "Content-Type: application/json" \
  -d '[{"content": "Write a hello world function in Python"}]' \
  2>/dev/null | head -20
echo ""
echo "..."
echo ""

echo "✅ API tests complete!"
echo ""
echo "To test interactively, you can use these commands:"
echo ""
echo "# Create a session:"
echo "curl -s -X POST $API_URL/api/sessions -H 'Content-Type: application/json' -d '{\"title\": \"My Session\"}' | jq"
echo ""
echo "# Run an agent (replace SESSION_ID and AGENT_NAME):"
echo "curl -X POST '$API_URL/api/sessions/SESSION_ID/agent/AGENT_NAME' -H 'Content-Type: application/json' -d '[{\"content\": \"Your message\"}]'"
echo ""
echo "Available agents: pirate, haiku, todo, code, gopher, shell, filesystem, pythonist, writer, dev-team, basic_agent"