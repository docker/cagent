# Cagent Authentication Guide

## Overview

Cagent now supports multi-user authentication with JWT tokens, allowing secure access control and session isolation. Each user can only access their own sessions, with admin users having full access.

## Features

- **JWT-based authentication** - Secure token-based authentication
- **User management** - Create, list, and manage users
- **Session isolation** - Users can only see and interact with their own sessions
- **Admin support** - Admin users have full access to all sessions
- **Backward compatibility** - Can run without authentication using `--disable-auth`

## Quick Start

### 1. Set JWT Secret

Set the JWT secret environment variable (required for authentication):

```bash
export CAGENT_JWT_SECRET="your-secret-key-here"
```

For production, use a strong, random secret:

```bash
export CAGENT_JWT_SECRET=$(openssl rand -base64 32)
```

### 2. Create First Admin User

```bash
cagent users create --admin
# Enter email, name, and password when prompted
```

### 3. Start API Server with Authentication

```bash
cagent api /path/to/agents --jwt-secret "$CAGENT_JWT_SECRET"
```

Or disable authentication for backward compatibility:

```bash
cagent api /path/to/agents --disable-auth
```

## API Authentication

### Register a New User

```bash
curl -X POST https://your-api/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "securepassword",
    "name": "John Doe"
  }'
```

Response:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "user-id",
    "email": "user@example.com",
    "name": "John Doe",
    "created_at": "2024-01-01T00:00:00Z",
    "is_admin": false
  }
}
```

### Login

```bash
curl -X POST https://your-api/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "securepassword"
  }'
```

Response (same as register):
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": { ... }
}
```

### Using the Token

Include the token in the Authorization header for all API requests:

```bash
curl https://your-api/api/sessions \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIs..."
```

## User Management CLI

### Create User

```bash
# Interactive mode
cagent users create

# Create admin user
cagent users create --admin
```

### List Users

```bash
cagent users list
```

### Delete User

```bash
cagent users delete user@example.com
```

### Grant Admin Privileges

```bash
cagent users make-admin user@example.com
```

### Specify Database Location

```bash
cagent users list --session-db /custom/path/sessions.db
```

## Cloud Run Deployment

### 1. Set JWT Secret in Secret Manager

```bash
echo -n "$(openssl rand -base64 32)" | gcloud secrets create cagent-jwt-secret --data-file=-
```

### 2. Deploy with Authentication

Update your deployment script or command:

```bash
gcloud run deploy cagent-api \
  --image=your-image \
  --set-secrets=CAGENT_JWT_SECRET=cagent-jwt-secret:latest \
  --args=api,/work,--listen=:8080,--session-db=/work/sessions.db
```

### 3. Create Initial Admin User

SSH into Cloud Run or use Cloud Shell:

```bash
# Download cagent binary or use Docker
docker run -it --rm \
  -v /work:/work \
  -e CAGENT_JWT_SECRET=$CAGENT_JWT_SECRET \
  your-image \
  users create --admin --session-db /work/sessions.db
```

## Python Client Example

```python
import requests
import json

class CagentClient:
    def __init__(self, api_url, email, password):
        self.api_url = api_url
        self.token = None
        self.login(email, password)
    
    def login(self, email, password):
        response = requests.post(
            f"{self.api_url}/api/auth/login",
            json={"email": email, "password": password}
        )
        response.raise_for_status()
        data = response.json()
        self.token = data["token"]
    
    def _headers(self):
        return {"Authorization": f"Bearer {self.token}"}
    
    def create_session(self, title):
        response = requests.post(
            f"{self.api_url}/api/sessions",
            json={"title": title},
            headers=self._headers()
        )
        response.raise_for_status()
        return response.json()
    
    def list_sessions(self):
        response = requests.get(
            f"{self.api_url}/api/sessions",
            headers=self._headers()
        )
        response.raise_for_status()
        return response.json()

# Usage
client = CagentClient(
    "https://YOUR-API-URL",
    "user@example.com",
    "password"
)

# Create a session
session = client.create_session("My Session")
print(f"Created session: {session['id']}")

# List your sessions
sessions = client.list_sessions()
print(f"You have {len(sessions)} sessions")
```

## Session Isolation

When authentication is enabled:

1. **Regular users** can only:
   - View their own sessions
   - Create sessions under their account
   - Delete their own sessions
   - Interact with agents in their sessions

2. **Admin users** can:
   - View all sessions from all users
   - Delete any session
   - Access user management endpoints
   - Override session ownership

## Migration from Existing Deployment

If you have an existing deployment without authentication:

1. **Keep existing sessions accessible**: Run with `--disable-auth` flag initially
2. **Create admin user**: Use the CLI to create your first admin user
3. **Gradually migrate**: Enable authentication and have users register
4. **Assign ownership**: Admins can see all old sessions (with empty user_id)

## Security Considerations

1. **JWT Secret**: 
   - Use a strong, random secret (at least 32 characters)
   - Never commit the secret to version control
   - Rotate the secret periodically

2. **Password Requirements**:
   - Minimum 8 characters (enforced by validation)
   - Passwords are hashed using bcrypt

3. **Token Expiration**:
   - Tokens expire after 24 hours
   - Users must re-authenticate after expiration

4. **HTTPS Only**:
   - Always use HTTPS in production
   - Never send tokens over unencrypted connections

## Troubleshooting

### "Authentication is disabled" error
- The server is running with `--disable-auth` flag
- Remove the flag to enable authentication

### "Invalid token" error
- Token may have expired (24-hour lifetime)
- Re-authenticate using `/api/auth/login`

### "User not found" after creating user
- Make sure you're using the same database file
- Check the `--session-db` path matches between commands

### Sessions not isolated
- Verify authentication is enabled (no `--disable-auth` flag)
- Check that tokens are being sent in requests
- Ensure database has been migrated (happens automatically on startup)