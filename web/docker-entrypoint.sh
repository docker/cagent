#!/bin/sh

# Use API_URL or CAGENT_API_URL (for backward compatibility)
API_ENDPOINT="${API_URL:-${CAGENT_API_URL}}"

# Replace placeholders with environment variables
if [ -n "$API_ENDPOINT" ]; then
    echo "Setting API URL to: $API_ENDPOINT"
    # Create config.js with the actual API URL
    cat > /usr/share/nginx/html/config.js <<EOF
// Configuration for the Cagent Chat Application
const Config = {
    // API base URL
    API_BASE_URL: '${API_ENDPOINT}',
    
    // Application settings
    MESSAGE_BATCH_SIZE: 50,
    SESSION_REFRESH_INTERVAL: 30000, // 30 seconds
    TOKEN_STORAGE_KEY: 'cagent_auth_token',
    USER_STORAGE_KEY: 'cagent_user',
    
    // Get full API URL
    getApiUrl(endpoint) {
        return \`\${this.API_BASE_URL}\${endpoint}\`;
    },
    
    // Check if running in development mode
    isDevelopment() {
        return window.location.hostname === 'localhost' || 
               window.location.hostname === '127.0.0.1';
    }
};

console.log('API Base URL:', Config.API_BASE_URL);
EOF
else
    echo "API URL not set, using default (same origin)"
    # Create config.js using window.location.origin
    cat > /usr/share/nginx/html/config.js <<'EOF'
// Configuration for the Cagent Chat Application
const Config = {
    // API base URL - defaults to same origin
    API_BASE_URL: window.location.origin,
    
    // Application settings
    MESSAGE_BATCH_SIZE: 50,
    SESSION_REFRESH_INTERVAL: 30000, // 30 seconds
    TOKEN_STORAGE_KEY: 'cagent_auth_token',
    USER_STORAGE_KEY: 'cagent_user',
    
    // Get full API URL
    getApiUrl(endpoint) {
        return `${this.API_BASE_URL}${endpoint}`;
    },
    
    // Check if running in development mode
    isDevelopment() {
        return window.location.hostname === 'localhost' || 
               window.location.hostname === '127.0.0.1';
    }
};

console.log('API Base URL:', Config.API_BASE_URL);
EOF
fi

# Remove the template file from public directory
rm -f /usr/share/nginx/html/config.js.template

# Start nginx
exec nginx -g 'daemon off;'