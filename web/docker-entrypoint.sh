#!/bin/sh

# Replace placeholders with environment variables
if [ -n "$CAGENT_API_URL" ]; then
    echo "Setting CAGENT_API_URL to: $CAGENT_API_URL"
    sed "s|__CAGENT_API_URL__|$CAGENT_API_URL|g" /usr/share/nginx/html/config.js.template > /usr/share/nginx/html/config.js
else
    echo "CAGENT_API_URL not set, using default (same origin)"
    sed "s|__CAGENT_API_URL__|window.location.origin|g" /usr/share/nginx/html/config.js.template > /usr/share/nginx/html/config.js
fi

# Remove the template file from public directory
rm -f /usr/share/nginx/html/config.js.template

# Start nginx
exec nginx -g 'daemon off;'