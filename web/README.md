# Cagent Web Chat Application

A modern web-based chat interface for interacting with Cagent AI agents.

## Features

- **User Authentication**: Secure registration and login with JWT tokens
- **Agent Selection**: Choose from available AI agents with descriptions
- **Session Management**: Create, resume, and delete chat sessions
- **Real-time Streaming**: Server-Sent Events (SSE) for streaming AI responses
- **Token Tracking**: Monitor token usage for each session
- **Responsive Design**: Works on desktop and mobile devices
- **Message Formatting**: Support for markdown, code blocks, and rich text

## Local Development

### Prerequisites
- Cagent API server running (default: http://localhost:8090)
- Modern web browser

### Quick Start

1. Start the Cagent API server:
```bash
cd ..
./cagent serve --port 8090
```

2. Serve the web files (using Python):
```bash
# Python 3
python -m http.server 8000

# Or using Node.js
npx http-server -p 8000
```

3. Open your browser to http://localhost:8000

### Configuration

The application can be configured in multiple ways:

1. **Environment Variable (Recommended for production)**:
   - Set `CAGENT_API_URL` when running the Docker container
   - This is processed at container startup and injected into the config

2. **Build-time Configuration**:
   - Edit `config.js.template` before building the Docker image

3. **Runtime Configuration** (for development):
   - Set `window.CAGENT_API_URL` in browser console before loading

## Docker Deployment

### Build the Docker image:
```bash
docker build -t cagent-web .
```

### Run locally:
```bash
# Without API URL (uses same origin)
docker run -p 8080:8080 cagent-web

# With specific API URL
docker run -p 8080:8080 -e CAGENT_API_URL=http://localhost:8090 cagent-web
```

### Deploy to Google Cloud Run:

1. Build and push to Google Container Registry:
```bash
# Set your project ID
PROJECT_ID=your-project-id

# Build and tag
docker build -t gcr.io/$PROJECT_ID/cagent-web .

# Push to GCR
docker push gcr.io/$PROJECT_ID/cagent-web
```

2. Deploy to Cloud Run:
```bash
# Without API URL (for same-service deployment)
gcloud run deploy cagent-web \
  --image gcr.io/$PROJECT_ID/cagent-web \
  --platform managed \
  --region us-central1 \
  --allow-unauthenticated \
  --port 8080

# With specific API URL (for separate services)
gcloud run deploy cagent-web \
  --image gcr.io/$PROJECT_ID/cagent-web \
  --platform managed \
  --region us-central1 \
  --allow-unauthenticated \
  --port 8080 \
  --set-env-vars="CAGENT_API_URL=https://cagent-api-xxxxx.run.app"

# Or use the deploy script
./deploy.sh cloudrun $PROJECT_ID us-central1 https://cagent-api-xxxxx.run.app
```

## Combined Deployment with API

For deploying both the API and web interface together on Cloud Run, create a combined Dockerfile in the parent directory that:
1. Builds the Go API server
2. Copies the web files
3. Uses nginx to serve both

Example combined nginx configuration:
- Serve static files from `/`
- Proxy `/api/*` requests to the Go server on port 8090

## Environment Variables

- `CAGENT_API_URL`: Sets the API endpoint URL for the web application
  - Used at container startup to configure the API connection
  - If not set, defaults to same origin (for co-located deployment)
  - Example: `CAGENT_API_URL=https://api.example.com`

## Security Notes

- JWT tokens are stored in localStorage
- Tokens expire after 24 hours
- All API requests require authentication (except registration/login)
- CORS is handled by the API server

## Browser Support

- Chrome/Edge 90+
- Firefox 88+
- Safari 14+
- Mobile browsers (iOS Safari, Chrome Mobile)

## Troubleshooting

### Cannot connect to API
- Ensure the Cagent API server is running
- Check CORS settings on the API server
- Verify the API URL in config.js

### Authentication issues
- Clear localStorage and try logging in again
- Check if the JWT secret is consistent between sessions

### Streaming not working
- Ensure your browser supports Server-Sent Events
- Check proxy settings if behind a reverse proxy
- Verify nginx configuration for SSE support

## Development

### File Structure
- `index.html` - Main HTML structure
- `styles.css` - All styling and responsive design
- `config.js` - Configuration settings
- `auth.js` - Authentication logic
- `api.js` - API communication layer
- `chat.js` - Chat functionality and session management
- `app.js` - Main application initialization

### Adding Features
1. Update the relevant JavaScript module
2. Add UI elements to index.html
3. Style in styles.css
4. Test locally before deploying

## License

Same as the main Cagent project.