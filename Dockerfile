# Build stage
FROM golang:1.25-alpine AS builder

# Install git and ca-certificates (needed for fetching dependencies and HTTPS)
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user with specific UID/GID for Synology compatibility
RUN addgroup -g 1026 appgroup && \
    adduser -D -s /bin/sh -u 1026 -G appgroup appuser

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Create files directory with proper permissions
RUN mkdir -p /app/files && chown -R 1026:1026 /app

# Install sudo for permission fixes (Alpine)
RUN apk add --no-cache sudo && \
    echo "appuser ALL=(root) NOPASSWD: /bin/chown" >> /etc/sudoers

# Switch to non-root user
USER appuser

# Expose port (optional, if you add web interface later)
EXPOSE 8080

# Set default environment variables
ENV STORAGE_PATH=/app/files

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD pgrep main || exit 1

# Set entrypoint and command
ENTRYPOINT ["./main"]
