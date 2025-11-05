# Build stage
FROM nginx:alpine

# Install bash for the entrypoint script
RUN apk add --no-cache bash

# Remove default nginx configuration
RUN rm /etc/nginx/conf.d/default.conf

# Copy nginx configuration (use standalone for separate deployment)
COPY nginx-standalone.conf /etc/nginx/conf.d/default.conf

# Copy web files
COPY *.html *.css /usr/share/nginx/html/
COPY *.js /usr/share/nginx/html/

# Copy config template
COPY config.js.template /usr/share/nginx/html/

# Copy and set permissions for entrypoint script
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Expose port 8080 for Cloud Run
EXPOSE 8080

# Use the entrypoint script to process environment variables
ENTRYPOINT ["/docker-entrypoint.sh"]
