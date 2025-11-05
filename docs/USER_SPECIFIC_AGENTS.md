# User-Specific Agents

The cagent API server now supports user-specific agents in addition to shared agents. This allows you to have both public agents available to all users and private agents available only to specific users.

## Directory Structure

Agents are organized in the following directory structure:

```
/work/                          # Root agents directory (configurable)
├── shared-agent1.yaml         # Shared agent available to all users
├── shared-agent2.yaml         # Another shared agent
├── user1@example.com/         # User-specific directory
│   ├── private-agent1.yaml    # Private agent for user1
│   └── private-agent2.yaml    # Another private agent for user1
└── user2@example.com/         # User-specific directory
    └── custom-agent.yaml      # Private agent for user2
```

## How It Works

1. **Shared Agents**: Any `.yaml` files in the root agents directory are available to all authenticated users.

2. **User-Specific Agents**: Each user can have their own subdirectory named after their email address. Agents in this directory are only visible and accessible to that specific user.

3. **Name Conflicts**: If a user has an agent with the same name as a shared agent, the user-specific agent takes precedence for that user.

## API Behavior

### GET /api/agents
Returns a combined list of:
- All shared agents from the root directory
- All user-specific agents from the user's directory (if it exists)
- User agents override shared agents with the same name

### POST /api/sessions
When creating a session, the system will:
1. First look for the agent in the shared agents directory
2. If not found, look in the user's specific directory
3. Return 404 if the agent is not found in either location

### GET /api/agents/:id
Will check both shared and user-specific directories, with user-specific taking precedence.

## Example Usage

### Starting the API Server
```bash
cagent api /work --jwt-secret mysecret
```

### Directory Setup for Google Cloud Storage
When using a GCS bucket mounted at `/work`:
```bash
# Create user-specific directory
mkdir -p /work/user@example.com

# Add a private agent
cp my-agent.yaml /work/user@example.com/

# Add a shared agent
cp shared-agent.yaml /work/
```

### Web Interface
The web application's agent dropdown will automatically show both:
- Shared agents (available to all users)
- User-specific agents (only for the logged-in user)

Users will see a unified list without needing to know whether an agent is shared or private.

## Security Considerations

1. **Authentication Required**: User-specific agents are only accessible when authentication is enabled (`--jwt-secret` flag must be set).

2. **Isolation**: Users cannot access other users' private agents.

3. **Email as Directory Name**: The user's email address is used as the directory name. Ensure your filesystem supports the characters in email addresses (most modern filesystems do).

4. **Permissions**: Ensure proper file system permissions are set on user directories if running in a multi-tenant environment.

## Benefits

1. **Personalization**: Users can have custom agents tailored to their specific needs.

2. **Privacy**: Sensitive or specialized agents can be kept private to specific users.

3. **Override Capability**: Users can override shared agents with their own versions.

4. **Scalability**: Easy to manage agents for multiple users in a cloud environment.

## Migration Guide

If you're upgrading from a version without user-specific agent support:

1. No changes are required for existing deployments
2. Shared agents continue to work as before
3. To add user-specific agents, simply create user directories as needed
4. The system is backward compatible

## Troubleshooting

### User agents not showing up
- Check that the directory name exactly matches the user's email address
- Verify file permissions allow the API server to read the files
- Check API server logs for any loading errors

### Agent conflicts
- User-specific agents always override shared agents with the same name
- Use unique names to avoid confusion

### Performance considerations
- Agent loading is cached and refreshed periodically
- Large numbers of agents may impact initial loading time
- Consider organizing agents into logical groups