#!/bin/bash

# Test cagent API with proper SSE streaming

API_URL="https://cagent-api-950783879036.us-central1.run.app"

echo "Creating a new session..."
SESSION_RESPONSE=$(curl -s -X POST "$API_URL/api/sessions" \
  -H "Content-Type: application/json" \
  -d '{"title": "Haiku Test"}')

SESSION_ID=$(echo "$SESSION_RESPONSE" | jq -r '.id')
echo "Session created: $SESSION_ID"

echo -e "\nSending request to generate a haiku about the cloud..."
AGENT_RESPONSE=$(curl -s -X POST "$API_URL/api/sessions/$SESSION_ID/agent/haiku" \
  -H "Content-Type: application/json" \
  -d '[{"content": "Write a haiku about cloud computing"}]')

echo -e "\n--- Streaming Response ---"
echo "$AGENT_RESPONSE" | while IFS= read -r line; do
    if [[ $line == data:* ]]; then
        # Extract JSON from SSE data line
        json_data="${line#data: }"
        
        # Parse and display based on event type
        if [[ -n "$json_data" && "$json_data" != " " ]]; then
            event_type=$(echo "$json_data" | jq -r '.type // empty' 2>/dev/null)
            
            case "$event_type" in
                "message_start")
                    echo "🚀 Message started"
                    ;;
                "content_block_delta")
                    content=$(echo "$json_data" | jq -r '.delta.text // empty' 2>/dev/null)
                    if [[ -n "$content" ]]; then
                        echo -n "$content"
                    fi
                    ;;
                "content_block_start")
                    echo -e "\n📝 Content block started"
                    ;;
                "content_block_stop")
                    echo -e "\n✅ Content block completed"
                    ;;
                "message_delta")
                    # Handle usage stats if present
                    usage=$(echo "$json_data" | jq -r '.usage // empty' 2>/dev/null)
                    if [[ -n "$usage" ]]; then
                        echo -e "\n📊 Token usage: $usage"
                    fi
                    ;;
                "message_stop")
                    echo -e "\n🏁 Message completed"
                    ;;
                "error")
                    error_msg=$(echo "$json_data" | jq -r '.error // .message // "Unknown error"' 2>/dev/null)
                    echo "❌ Error: $error_msg"
                    ;;
                *)
                    if [[ -n "$event_type" ]]; then
                        echo "ℹ️ Event: $event_type"
                    fi
                    ;;
            esac
        fi
    fi
done

echo -e "\n--- End of Stream ---"