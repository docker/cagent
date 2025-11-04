# WARP Agent Instructions for cagent

## Testing with Docker

### Building and Running in a Clean Docker Container

The project includes a Dockerfile that builds a lightweight Alpine-based container with the cagent binary. Here's how to test the project using Docker:

#### 1. Build the Docker Image

```bash
# Build for current platform
docker build -t cagent:test .

# Or use buildx for multi-platform
docker buildx build --platform linux/amd64,linux/arm64 -t cagent:test .
```

#### 2. Run cagent in a Container

```bash
# Run with a config file from host
docker run -it --rm \
  -v $(pwd)/examples:/work/examples:ro \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  cagent:test run examples/basic_agent.yaml

# Run with interactive shell for testing
docker run -it --rm \
  -v $(pwd):/work:ro \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  --entrypoint /bin/sh \
  cagent:test

# Inside container, you can then run:
# /cagent run examples/basic_agent.yaml
```

#### 3. Using docker exec for Testing Inside Container

If you want to keep a container running and exec into it for testing:

```bash
# Start container in background with sleep
docker run -d --name cagent-test \
  -v $(pwd):/work:ro \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  --entrypoint sleep \
  cagent:test infinity

# Exec into the container for testing
docker exec -it cagent-test /bin/sh

# Inside container:
cd /work
/cagent run examples/basic_agent.yaml
/cagent run examples/pirate.yaml
# Test different configurations...

# When done, stop and remove
docker stop cagent-test
docker rm cagent-test
```

#### 4. Testing with Docker Compose (for development with Jaeger)

The project includes a docker-compose.yaml for running Jaeger (distributed tracing):

```bash
# Start Jaeger for tracing
docker compose up -d

# Run cagent with OpenTelemetry enabled
docker run -it --rm \
  --network host \
  -v $(pwd)/examples:/work/examples:ro \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  cagent:test run examples/basic_agent.yaml --otel

# View traces at http://localhost:16686
```

### Container Features

The Docker image includes:
- Alpine Linux base (lightweight)
- ca-certificates for HTTPS
- docker-cli for Docker-in-Docker scenarios
- MCP Gateway plugin pre-installed
- Non-root user (cagent) for security
- `/data` and `/work` directories with appropriate permissions

### Environment Variables in Container

The container sets:
- `DOCKER_MCP_IN_CONTAINER=1` - Indicates running inside Docker
- `TERM=xterm-256color` - For better TUI support

### Volume Mounts

Common volume mounts:
- Config files: `-v $(pwd)/examples:/work/examples:ro`
- Data persistence: `-v cagent-data:/data`
- Full project (read-only): `-v $(pwd):/work:ro`

## Development Guidelines

When developing cagent features, always test in both:
1. Local environment (direct binary)
2. Docker container (isolated environment)

This ensures compatibility across different deployment scenarios.