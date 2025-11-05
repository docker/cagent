#!/bin/bash

# Cagent Web Deployment Script

set -e

echo "Cagent Web Deployment Script"
echo "============================"
echo ""

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed. Please install Docker first."
    exit 1
fi

# Parse command line arguments
ACTION=${1:-"local"}
PROJECT_ID=${2:-""}
REGION=${3:-"us-central1"}

case $ACTION in
    "local")
        echo "Building and running locally..."
        docker build -t cagent-web .
        echo ""
        echo "Starting web server on port 8080..."
        if [ -n "$CAGENT_API_URL" ]; then
            echo "Using CAGENT_API_URL: $CAGENT_API_URL"
            docker run -p 8080:8080 -e CAGENT_API_URL="$CAGENT_API_URL" cagent-web
        else
            echo "No CAGENT_API_URL set, will use same origin"
            docker run -p 8080:8080 cagent-web
        fi
        ;;
        
    "build")
        echo "Building Docker image..."
        docker build -t cagent-web .
        echo "Build complete!"
        ;;
        
    "gcr")
        if [ -z "$PROJECT_ID" ]; then
            echo "Error: Please provide a Google Cloud PROJECT_ID"
            echo "Usage: ./deploy.sh gcr PROJECT_ID [REGION]"
            exit 1
        fi
        
        echo "Building and pushing to Google Container Registry..."
        echo "Project ID: $PROJECT_ID"
        echo "Region: $REGION"
        
        # Build and tag
        docker build -t gcr.io/$PROJECT_ID/cagent-web .
        
        # Push to GCR
        docker push gcr.io/$PROJECT_ID/cagent-web
        
        echo ""
        echo "Image pushed successfully!"
        echo "To deploy to Cloud Run, run:"
        echo "  ./deploy.sh cloudrun $PROJECT_ID $REGION"
        ;;
        
    "cloudrun")
        if [ -z "$PROJECT_ID" ]; then
            echo "Error: Please provide a Google Cloud PROJECT_ID"
            echo "Usage: ./deploy.sh cloudrun PROJECT_ID [REGION] [API_URL]"
            exit 1
        fi
        
        API_URL=${4:-""}
        
        echo "Deploying to Google Cloud Run..."
        echo "Project ID: $PROJECT_ID"
        echo "Region: $REGION"
        
        if [ -n "$API_URL" ]; then
            echo "API URL: $API_URL"
            gcloud run deploy cagent-web \
                --image gcr.io/$PROJECT_ID/cagent-web \
                --platform managed \
                --region $REGION \
                --allow-unauthenticated \
                --port 8080 \
                --memory 256Mi \
                --cpu 1 \
                --max-instances 10 \
                --set-env-vars="CAGENT_API_URL=$API_URL"
        else
            echo "No API URL specified, will use same origin"
            gcloud run deploy cagent-web \
                --image gcr.io/$PROJECT_ID/cagent-web \
                --platform managed \
                --region $REGION \
                --allow-unauthenticated \
                --port 8080 \
                --memory 256Mi \
                --cpu 1 \
                --max-instances 10
        fi
        
        echo ""
        echo "Deployment complete!"
        ;;
        
    "dev")
        echo "Starting development server..."
        echo "Make sure the Cagent API is running on port 8090"
        echo ""
        
        # Check if Python is available
        if command -v python3 &> /dev/null; then
            echo "Starting Python HTTP server on port 8000..."
            python3 -m http.server 8000
        elif command -v python &> /dev/null; then
            echo "Starting Python HTTP server on port 8000..."
            python -m SimpleHTTPServer 8000
        else
            echo "Python not found. Please install Python or use another HTTP server."
            echo "Alternative: npx http-server -p 8000"
            exit 1
        fi
        ;;
        
    *)
        echo "Usage: ./deploy.sh [action] [options]"
        echo ""
        echo "Actions:"
        echo "  local              - Build and run Docker container locally (default)"
        echo "  build              - Build Docker image only"
        echo "  gcr PROJECT_ID     - Build and push to Google Container Registry"
        echo "  cloudrun PROJECT_ID [REGION] [API_URL] - Deploy to Google Cloud Run"
        echo "  dev                - Start development server (Python)"
        echo ""
        echo "Environment Variables:"
        echo "  CAGENT_API_URL     - Set the API endpoint URL"
        echo ""
        echo "Examples:"
        echo "  ./deploy.sh local"
        echo "  CAGENT_API_URL=http://localhost:8090 ./deploy.sh local"
        echo "  ./deploy.sh gcr my-project-123"
        echo "  ./deploy.sh cloudrun my-project-123 us-central1"
        echo "  ./deploy.sh cloudrun my-project-123 us-central1 https://api.example.com"
        echo "  ./deploy.sh dev"
        ;;
esac