#!/bin/bash

API_URL="https://YOUR-API-URL"

echo "Creating session..."
SESSION_ID=$(curl -s -X POST "$API_URL/api/sessions" \
  -H "Content-Type: application/json" \
  -d '{"title": "Debug Test"}' | jq -r '.id')

echo "Session: $SESSION_ID"
echo ""
echo "Sending request and capturing raw response..."
echo "================================================"

curl -X POST "$API_URL/api/sessions/$SESSION_ID/agent/basic_agent" \
  -H "Content-Type: application/json" \
  -d '[{"content": "What is 2 + 2?"}]' 2>/dev/null

echo ""
echo "================================================"
echo ""
echo "Session info:"
curl -s "$API_URL/api/sessions/$SESSION_ID" | jq '{id, title, input_tokens, output_tokens}'