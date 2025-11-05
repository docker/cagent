// Configuration for the Cagent Chat Application
const Config = {
    // API base URL - can be overridden by environment variable
    API_BASE_URL: window.CAGENT_API_URL || window.location.origin,
    
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

// For Google Cloud Run deployment, detect the service URL
if (window.location.hostname.includes('run.app')) {
    Config.API_BASE_URL = window.location.origin;
}

console.log('API Base URL:', Config.API_BASE_URL);