# Cagent Cloud Run Deployment

## Overview
Cagent has been successfully deployed to Google Cloud Run with API mode exposed to the internet. The deployment includes Google Cloud Storage (GCS) bucket mounting for persistent YAML agent configurations.

## Deployment Details

### Service Information
- **Service Name:** cagent-api
- **GCP Project:** `YOUR-PROJECT-ID`
- **Region:** `YOUR-REGION` (e.g., us-central1)
- **Public API URL:** `https://cagent-api-XXXXX.YOUR-REGION.run.app`
- **Authentication:** Application-level JWT authentication

### Infrastructure Components
1. **Container Image:** `YOUR-REGION-docker.pkg.dev/YOUR-PROJECT-ID/YOUR-REPO/cagent:latest`
2. **GCS Bucket:** `gs://YOUR-BUCKET-NAME` (mounted at `/work` in the container)
   - Stores YAML agent configurations
   - Stores SQLite session database (`sessions.db`)
3. **Secret Manager:** 
   - `openai-api-key` - OpenAI API credentials
   - `anthropic-api-key` - Anthropic API credentials

### Resource Allocation
- **CPU:** 2 vCPUs
- **Memory:** 2 GiB
- **Execution Environment:** Gen2 (with Cloud Storage FUSE support)

## API Endpoints

### Health Check
```bash
curl https://YOUR-API-URL/api/ping
```

### List Available Agents
```bash
curl https://YOUR-API-URL/api/agents \
  -H "Authorization: Bearer YOUR-JWT-TOKEN"
```

### Create a Session
```bash
curl -X POST https://YOUR-API-URL/api/sessions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR-JWT-TOKEN" \
  -d '{"title": "My Session", "workingDir": "/work"}'
```

### Run an Agent
```bash
SESSION_ID="<session-id-from-create>"
AGENT_NAME="pirate.yaml"  # or any agent from the list

curl -X POST "https://YOUR-API-URL/api/sessions/$SESSION_ID/agent/$AGENT_NAME" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR-JWT-TOKEN" \
  -d '[{"content": "Your message here"}]'
```

## Managing Agent Configurations

### Upload New Agent
```bash
# Upload a YAML file to the GCS bucket
gcloud storage cp my-agent.yaml gs://YOUR-BUCKET-NAME/
```

### List Agents in Bucket
```bash
gcloud storage ls gs://YOUR-BUCKET-NAME/
```

### Update Agent Configuration
```bash
# Replace existing agent
gcloud storage cp my-updated-agent.yaml gs://YOUR-BUCKET-NAME/my-agent.yaml
```

### Remove Agent
```bash
gcloud storage rm gs://YOUR-BUCKET-NAME/my-agent.yaml
```

## Monitoring and Maintenance

### View Service Logs
```bash
# Using gcloud logging
gcloud logging read "resource.type=cloud_run_revision AND resource.labels.service_name=cagent-api" \
  --limit=50 \
  --format="table(timestamp,severity,textPayload)"
```

### Check Service Status
```bash
gcloud run services describe cagent-api --region us-central1
```

### Update Service
```bash
# To redeploy with new image
docker buildx build --platform linux/amd64 -t YOUR-REGION-docker.pkg.dev/YOUR-PROJECT-ID/YOUR-REPO/cagent:latest --push .

# Redeploy service (will use latest image)
gcloud run services update cagent-api --region YOUR-REGION --image YOUR-REGION-docker.pkg.dev/YOUR-PROJECT-ID/YOUR-REPO/cagent:latest
```

## Key Features

1. **Persistent Storage:** 
   - YAML agent configurations stored in GCS bucket
   - SQLite session database (`/work/sessions.db`) persisted in the same GCS bucket
   - All data persists across container restarts and deployments
2. **Dynamic Loading:** New agents added to the bucket are immediately available via the API
3. **Session Persistence:** Chat sessions are saved to SQLite and survive container restarts
4. **Public API Access:** The API is accessible from the internet without authentication
5. **Secure Secrets:** API keys are stored in Google Secret Manager and injected as environment variables
6. **Auto-scaling:** Cloud Run automatically scales based on traffic

## Security Considerations

- The API endpoint is publicly accessible - consider adding authentication if needed
- API keys are securely stored in Secret Manager
- The GCS bucket is only accessible from the Cloud Run service
- Consider implementing rate limiting for production use

## Troubleshooting

### Service Not Responding
1. Check service logs for errors
2. Verify the container is running: `gcloud run services describe cagent-api --region us-central1`
3. Check if the GCS bucket is accessible

### Agents Not Loading
1. Verify YAML files exist in the bucket: `gcloud storage ls gs://YOUR-BUCKET-NAME/`
2. Check file permissions and format
3. Review service logs for parsing errors

### Authentication Issues
1. Verify secrets are properly configured: `gcloud secrets list`
2. Check if the service account has access to secrets
3. Ensure API keys are valid and not expired

## Web Frontend Configuration

### IMPORTANT: Production API Usage

**The web frontend MUST use the PRODUCTION API with authentication enabled.**
- The test API (`cagent-api-test`) is ONLY for automated testing, NOT for the web frontend
- The production API (`cagent-api`) handles authentication at the application level using JWT tokens

### Environment Variables

**IMPORTANT**: When deploying the cagent-web service, you MUST:

1. Use the correct environment variable name: `CAGENT_API_URL` (not `API_URL` or any other variant)
2. Point it to the PRODUCTION API: `https://YOUR-PRODUCTION-API-URL`
3. NEVER point the web frontend to the test API

### Example Web Deployment

```bash
# Deploy or update cagent-web with PRODUCTION API URL
gcloud run services update cagent-web \
  --region YOUR-REGION \
  --update-env-vars "CAGENT_API_URL=https://YOUR-PRODUCTION-API-URL"
```

### CORS Configuration

The API server is configured to allow CORS requests from:
- Your production web frontend URL
- `http://localhost:8000` (local development)
- `http://localhost:8080` (Docker local)

When deploying, update the CORS configuration in `pkg/server/server.go` to include your actual web frontend URL.

## Deployment Environments

### Production Environment (FOR WEB FRONTEND)
- **API Service**: `cagent-api`
  - URL: `https://YOUR-PRODUCTION-API-URL`
  - Cloud Run Access: Public (required for CORS preflight requests)
  - Application Authentication: **ENABLED** - JWT tokens required
  - Configuration: No `--disable-auth` flag
  - **USAGE**: This is the ONLY API the web frontend should connect to

### Test Environment (FOR AUTOMATED TESTING ONLY)
- **API Service**: `cagent-api-test`
  - URL: `https://YOUR-TEST-API-URL`
  - Cloud Run Access: Public
  - Application Authentication: **DISABLED** (for automated testing)
  - Configuration: Uses `--disable-auth` flag
  - **WARNING**: Only use for automated testing, NEVER for the web frontend or production data

## Future Enhancements

1. Add authentication/authorization for API access
2. Implement rate limiting and quotas
3. Set up monitoring dashboards in Cloud Monitoring
4. Configure alerts for service health and errors
5. Add CI/CD pipeline for automatic deployments
6. Implement backup strategy for GCS bucket
