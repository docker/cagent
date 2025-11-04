#!/bin/bash

# Cagent GCP Deployment Script
# Deploys cagent to Google Cloud Run with GCS bucket for YAML configurations
#
# Usage: ./deploy_to_gcp.sh <GCP_PROJECT_ID> [GCS_BUCKET_NAME]
# Examples:
#   ./deploy_to_gcp.sh my-project-id
#   ./deploy_to_gcp.sh my-project-id my-custom-bucket
#   ./deploy_to_gcp.sh my-project-id --dry-run
#   ./deploy_to_gcp.sh my-project-id --cleanup

set -e
set -o pipefail

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
SERVICE_NAME="cagent-api"
REGION="us-central1"
REPO_NAME="cagent-repo"
DEFAULT_BUCKET_PREFIX="cagent-data"
IMAGE_TAG=""
DRY_RUN=false
CLEANUP=false
DEPLOYMENT_INFO_FILE=".gcp_deployment_info"

# Functions
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

show_help() {
    cat << EOF
Cagent GCP Deployment Script

Usage: $0 <GCP_PROJECT_ID> [GCS_BUCKET_NAME] [OPTIONS]

Arguments:
  GCP_PROJECT_ID    Required. The Google Cloud Project ID to deploy to
  GCS_BUCKET_NAME   Optional. Name for the GCS bucket (auto-generated if not provided)

Options:
  --dry-run         Preview actions without executing them
  --cleanup         Remove all deployed resources
  --help            Show this help message

Examples:
  $0 my-project-id
  $0 my-project-id my-custom-bucket
  $0 my-project-id --dry-run
  $0 my-project-id --cleanup

EOF
    exit 0
}

# Check prerequisites
check_prerequisites() {
    print_info "Checking prerequisites..."
    
    # Check gcloud
    if ! command -v gcloud &> /dev/null; then
        print_error "gcloud CLI is not installed. Please install it from: https://cloud.google.com/sdk/docs/install"
    fi
    
    # Check Docker
    if ! command -v docker &> /dev/null; then
        print_error "Docker is not installed. Please install Docker from: https://docs.docker.com/get-docker/"
    fi
    
    # Check if Docker is running
    if ! docker info &> /dev/null; then
        print_error "Docker daemon is not running. Please start Docker."
    fi
    
    # Check gcloud authentication
    if ! gcloud auth list --filter=status:ACTIVE --format="value(account)" &> /dev/null; then
        print_error "Not authenticated with gcloud. Please run: gcloud auth login"
    fi
    
    print_success "All prerequisites met"
}

# Validate and set GCP project
validate_project() {
    local project_id=$1
    print_info "Validating GCP project: $project_id"
    
    if [ "$DRY_RUN" = true ]; then
        print_info "[DRY-RUN] Would validate project: $project_id"
        return
    fi
    
    if ! gcloud projects describe "$project_id" &> /dev/null; then
        print_error "Project '$project_id' does not exist or you don't have access to it"
    fi
    
    gcloud config set project "$project_id"
    print_success "Project validated and set: $project_id"
}

# Generate bucket name if not provided
generate_bucket_name() {
    local timestamp=$(date +%Y%m%d-%H%M%S)
    echo "${DEFAULT_BUCKET_PREFIX}-${timestamp}"
}

# Create or validate GCS bucket
setup_gcs_bucket() {
    local bucket_name=$1
    print_info "Setting up GCS bucket: gs://$bucket_name"
    
    if [ "$DRY_RUN" = true ]; then
        print_info "[DRY-RUN] Would create/validate bucket: gs://$bucket_name"
        return
    fi
    
    # Check if bucket exists
    if gcloud storage buckets describe "gs://$bucket_name" &> /dev/null; then
        print_info "Bucket already exists: gs://$bucket_name"
    else
        print_info "Creating new bucket: gs://$bucket_name"
        gcloud storage buckets create "gs://$bucket_name" \
            --location="$REGION" \
            --uniform-bucket-level-access
        print_success "Bucket created successfully"
    fi
    
    # Copy example YAML files to bucket (if examples directory exists)
    if [ -d "examples" ]; then
        print_info "Copying example YAML files to bucket..."
        gcloud storage cp examples/*.yaml "gs://$bucket_name/" 2>/dev/null || true
        print_success "Example files copied to bucket"
    fi
}

# Setup Artifact Registry repository
setup_artifact_registry() {
    local project_id=$1
    print_info "Setting up Artifact Registry repository..."
    
    if [ "$DRY_RUN" = true ]; then
        print_info "[DRY-RUN] Would create/validate Artifact Registry repository: $REPO_NAME"
        return
    fi
    
    # Enable Artifact Registry API
    gcloud services enable artifactregistry.googleapis.com
    
    # Check if repository exists
    if gcloud artifacts repositories describe "$REPO_NAME" --location="$REGION" &> /dev/null 2>&1; then
        print_info "Repository already exists: $REPO_NAME"
    else
        print_info "Creating Artifact Registry repository: $REPO_NAME"
        gcloud artifacts repositories create "$REPO_NAME" \
            --repository-format=docker \
            --location="$REGION" \
            --description="Cagent Docker images"
        print_success "Repository created successfully"
    fi
    
    # Configure Docker authentication
    gcloud auth configure-docker "$REGION-docker.pkg.dev"
}

# Build and push Docker image
build_and_push_image() {
    local project_id=$1
    local git_commit=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    local timestamp=$(date +%Y%m%d-%H%M%S)
    IMAGE_TAG="${git_commit}-${timestamp}"
    local full_image_path="$REGION-docker.pkg.dev/$project_id/$REPO_NAME/cagent"
    
    print_info "Building Docker image with tag: $IMAGE_TAG"
    
    if [ "$DRY_RUN" = true ]; then
        print_info "[DRY-RUN] Would build and push image: $full_image_path:$IMAGE_TAG"
        return
    fi
    
    # Build for linux/amd64
    docker buildx build \
        --platform linux/amd64 \
        --build-arg GIT_TAG="$IMAGE_TAG" \
        --build-arg GIT_COMMIT="$git_commit" \
        -t "$full_image_path:$IMAGE_TAG" \
        -t "$full_image_path:latest" \
        --push \
        .
    
    print_success "Image built and pushed successfully"
}

# Setup Secret Manager
setup_secrets() {
    local project_id=$1
    print_info "Setting up Secret Manager..."
    
    if [ "$DRY_RUN" = true ]; then
        print_info "[DRY-RUN] Would setup Secret Manager secrets"
        return
    fi
    
    # Enable Secret Manager API
    gcloud services enable secretmanager.googleapis.com
    
    # Check for OpenAI API key
    if ! gcloud secrets describe openai-api-key &> /dev/null 2>&1; then
        if [ -n "$OPENAI_API_KEY" ]; then
            print_info "Creating secret: openai-api-key"
            echo -n "$OPENAI_API_KEY" | gcloud secrets create openai-api-key --data-file=-
            print_success "OpenAI API key secret created"
        else
            print_warning "OPENAI_API_KEY environment variable not found. Please create the secret manually:"
            print_warning "  echo -n 'YOUR_API_KEY' | gcloud secrets create openai-api-key --data-file=-"
        fi
    else
        print_info "Secret already exists: openai-api-key"
    fi
    
    # Anthropic API key is optional (user mentioned they don't have one)
    if ! gcloud secrets describe anthropic-api-key &> /dev/null 2>&1; then
        if [ -n "$ANTHROPIC_API_KEY" ]; then
            print_info "Creating secret: anthropic-api-key"
            echo -n "$ANTHROPIC_API_KEY" | gcloud secrets create anthropic-api-key --data-file=-
            print_success "Anthropic API key secret created"
        else
            print_info "Anthropic API key not provided (optional)"
        fi
    else
        print_info "Secret already exists: anthropic-api-key"
    fi
}

# Deploy to Cloud Run
deploy_to_cloud_run() {
    local project_id=$1
    local bucket_name=$2
    local full_image_path="$REGION-docker.pkg.dev/$project_id/$REPO_NAME/cagent"
    
    print_info "Deploying to Cloud Run..."
    
    if [ "$DRY_RUN" = true ]; then
        print_info "[DRY-RUN] Would deploy service: $SERVICE_NAME"
        print_info "[DRY-RUN] Image: $full_image_path:latest"
        print_info "[DRY-RUN] Bucket mount: gs://$bucket_name -> /work"
        return
    fi
    
    # Enable required APIs
    gcloud services enable run.googleapis.com
    
    # Build the gcloud run deploy command
    local deploy_cmd="gcloud run deploy $SERVICE_NAME \
        --image=$full_image_path:latest \
        --region=$REGION \
        --platform=managed \
        --allow-unauthenticated \
        --cpu=2 \
        --memory=2Gi \
        --timeout=3600 \
        --max-instances=10 \
        --execution-environment=gen2 \
        --add-volume=name=agent-configs,type=cloud-storage,bucket=$bucket_name \
        --add-volume-mount=volume=agent-configs,mount-path=/work \
        --command=/cagent \
        --args=api,/work,--listen=:8080,--session-db=/work/sessions.db"
    
    # Add secrets if they exist
    if gcloud secrets describe openai-api-key &> /dev/null 2>&1; then
        deploy_cmd="$deploy_cmd --set-secrets=OPENAI_API_KEY=openai-api-key:latest"
    fi
    
    if gcloud secrets describe anthropic-api-key &> /dev/null 2>&1; then
        deploy_cmd="$deploy_cmd --update-secrets=ANTHROPIC_API_KEY=anthropic-api-key:latest"
    fi
    
    # Execute deployment
    eval $deploy_cmd
    
    print_success "Service deployed successfully"
}

# Verify deployment
verify_deployment() {
    local project_id=$1
    local bucket_name=$2
    
    print_info "Verifying deployment..."
    
    if [ "$DRY_RUN" = true ]; then
        print_info "[DRY-RUN] Would verify deployment"
        return
    fi
    
    # Get service URL
    local service_url=$(gcloud run services describe $SERVICE_NAME --region=$REGION --format="value(status.url)")
    
    if [ -z "$service_url" ]; then
        print_error "Failed to retrieve service URL"
    fi
    
    # Test health endpoint
    print_info "Testing health endpoint..."
    if curl -s -o /dev/null -w "%{http_code}" "$service_url/api/ping" | grep -q "200"; then
        print_success "Health check passed"
    else
        print_warning "Health check failed - service may still be starting"
    fi
    
    # Save deployment information
    cat > "$DEPLOYMENT_INFO_FILE" << EOF
# Cagent GCP Deployment Information
# Generated: $(date)

PROJECT_ID=$project_id
BUCKET_NAME=$bucket_name
SERVICE_NAME=$SERVICE_NAME
REGION=$REGION
SERVICE_URL=$service_url
IMAGE_TAG=$IMAGE_TAG

# Commands for management:
# View logs:
gcloud logging read "resource.type=cloud_run_revision AND resource.labels.service_name=$SERVICE_NAME" --limit=50 --format="table(timestamp,severity,textPayload)"

# Update service:
gcloud run services update $SERVICE_NAME --region=$REGION

# List agents:
curl $service_url/api/agents

# Upload new agent:
gcloud storage cp your-agent.yaml gs://$bucket_name/

# Delete service (cleanup):
gcloud run services delete $SERVICE_NAME --region=$REGION
EOF
    
    # Display deployment summary
    echo ""
    print_success "🚀 Deployment Complete!"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${GREEN}Service Information:${NC}"
    echo "  • Service URL: $service_url"
    echo "  • GCS Bucket: gs://$bucket_name"
    echo "  • Image Tag: $IMAGE_TAG"
    echo "  • Region: $REGION"
    echo ""
    echo -e "${BLUE}Quick Commands:${NC}"
    echo "  • List agents: curl $service_url/api/agents"
    echo "  • View logs: gcloud logging read \"resource.labels.service_name=$SERVICE_NAME\" --limit=20"
    echo "  • Upload agent: gcloud storage cp agent.yaml gs://$bucket_name/"
    echo ""
    echo -e "${YELLOW}Note:${NC} Deployment details saved to: $DEPLOYMENT_INFO_FILE"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# Cleanup function
cleanup_resources() {
    local project_id=$1
    
    print_warning "⚠️  This will delete all cagent resources from GCP!"
    read -p "Are you sure you want to continue? (yes/no): " confirm
    
    if [ "$confirm" != "yes" ]; then
        print_info "Cleanup cancelled"
        exit 0
    fi
    
    # Load deployment info if exists
    if [ -f "$DEPLOYMENT_INFO_FILE" ]; then
        source "$DEPLOYMENT_INFO_FILE"
    fi
    
    print_info "Starting cleanup..."
    
    # Delete Cloud Run service
    if gcloud run services describe $SERVICE_NAME --region=$REGION &> /dev/null 2>&1; then
        print_info "Deleting Cloud Run service..."
        gcloud run services delete $SERVICE_NAME --region=$REGION --quiet
        print_success "Service deleted"
    fi
    
    # Delete GCS bucket (if known)
    if [ -n "$BUCKET_NAME" ]; then
        print_info "Deleting GCS bucket: gs://$BUCKET_NAME"
        read -p "Delete bucket and all contents? (yes/no): " delete_bucket
        if [ "$delete_bucket" = "yes" ]; then
            gcloud storage rm -r "gs://$BUCKET_NAME"
            print_success "Bucket deleted"
        fi
    fi
    
    # Note: We don't delete the Artifact Registry or secrets as they might be shared
    print_warning "Note: Artifact Registry and secrets were preserved (may be shared resources)"
    
    # Remove deployment info file
    rm -f "$DEPLOYMENT_INFO_FILE"
    
    print_success "Cleanup completed"
}

# Main script
main() {
    # Parse arguments
    if [ $# -lt 1 ]; then
        show_help
    fi
    
    GCP_PROJECT_ID=$1
    GCS_BUCKET_NAME=""
    
    # Parse additional arguments
    shift
    while [ $# -gt 0 ]; do
        case $1 in
            --dry-run)
                DRY_RUN=true
                print_info "Running in DRY-RUN mode - no changes will be made"
                ;;
            --cleanup)
                CLEANUP=true
                ;;
            --help)
                show_help
                ;;
            -*)
                print_error "Unknown option: $1"
                ;;
            *)
                if [ -z "$GCS_BUCKET_NAME" ]; then
                    GCS_BUCKET_NAME=$1
                fi
                ;;
        esac
        shift
    done
    
    # If cleanup mode, run cleanup and exit
    if [ "$CLEANUP" = true ]; then
        cleanup_resources "$GCP_PROJECT_ID"
        exit 0
    fi
    
    # Generate bucket name if not provided
    if [ -z "$GCS_BUCKET_NAME" ]; then
        GCS_BUCKET_NAME=$(generate_bucket_name)
        print_info "Generated bucket name: $GCS_BUCKET_NAME"
    fi
    
    # Start deployment
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${BLUE}Cagent GCP Deployment${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Project: $GCP_PROJECT_ID"
    echo "Bucket: $GCS_BUCKET_NAME"
    echo "Region: $REGION"
    if [ "$DRY_RUN" = true ]; then
        echo "Mode: DRY-RUN"
    fi
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    
    # Run deployment steps
    check_prerequisites
    validate_project "$GCP_PROJECT_ID"
    setup_gcs_bucket "$GCS_BUCKET_NAME"
    setup_artifact_registry "$GCP_PROJECT_ID"
    build_and_push_image "$GCP_PROJECT_ID"
    setup_secrets "$GCP_PROJECT_ID"
    deploy_to_cloud_run "$GCP_PROJECT_ID" "$GCS_BUCKET_NAME"
    verify_deployment "$GCP_PROJECT_ID" "$GCS_BUCKET_NAME"
}

# Run main function
main "$@"