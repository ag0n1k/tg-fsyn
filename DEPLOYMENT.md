# Deployment Guide

This guide covers different deployment scenarios for the Telegram File Storage Bot, from development to production environments.

## Table of Contents

- [Quick Start](#quick-start)
- [Local Development](#local-development)
- [Docker Deployment](#docker-deployment)
- [Production Deployment](#production-deployment)
- [Cloud Deployment](#cloud-deployment)
- [Monitoring and Maintenance](#monitoring-and-maintenance)
- [Troubleshooting](#troubleshooting)

## Quick Start

### Using the Start Script

The easiest way to get started:

```bash
# Initial setup
./start.sh setup

# Edit .env file with your bot token
nano .env

# Start the bot (auto-detects best method)
./start.sh start

# Check status
./start.sh status

# View logs
./start.sh logs
```

## Local Development

### Prerequisites

- Go 1.21 or higher
- Git

### Setup

1. **Clone and setup:**
   ```bash
   git clone <repository-url>
   cd tg-fsyn
   cp .env.example .env
   ```

2. **Configure environment:**
   ```bash
   # Edit .env file
   nano .env
   
   # Or set environment variables directly
   export TELEGRAM_BOT_TOKEN="your_bot_token_here"
   export STORAGE_PATH="./files"
   ```

3. **Install dependencies:**
   ```bash
   go mod tidy
   ```

4. **Run the bot:**
   ```bash
   # Direct run
   go run main.go
   
   # Or using Makefile
   make run
   
   # Or using start script
   ./start.sh start-local
   ```

### Development Tools

Install useful development tools:

```bash
# Install all development tools
make install-tools

# Or install individually
go install github.com/cosmtrek/air@latest          # Live reload
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest  # Linting
go install golang.org/x/tools/cmd/goimports@latest # Import formatting
```

### Live Reload Development

```bash
# Start with live reload (requires air)
make dev

# Or manually with air
air
```

## Docker Deployment

### Simple Docker

1. **Build and run:**
   ```bash
   # Build image
   docker build -t tg-file-bot .
   
   # Run container
   docker run -d \
     --name tg-file-bot \
     --restart unless-stopped \
     -e TELEGRAM_BOT_TOKEN="your_token_here" \
     -v $(pwd)/files:/app/files \
     tg-file-bot
   ```

2. **Manage container:**
   ```bash
   # View logs
   docker logs -f tg-file-bot
   
   # Stop container
   docker stop tg-file-bot
   docker rm tg-file-bot
   ```

### Docker Compose (Recommended)

1. **Setup:**
   ```bash
   # Copy environment file
   cp .env.example .env
   nano .env  # Edit with your bot token
   ```

2. **Deploy:**
   ```bash
   # Start services
   docker-compose up -d
   
   # View logs
   docker-compose logs -f
   
   # Stop services
   docker-compose down
   
   # Restart services
   docker-compose restart
   ```

3. **Update deployment:**
   ```bash
   # Rebuild and restart
   docker-compose up -d --build
   ```

### Using Makefile with Docker

```bash
# Docker commands via Makefile
make docker-build
make docker-run
make docker-logs
make docker-stop

# Docker Compose commands
make compose-up
make compose-logs
make compose-down
make compose-build
```

## Production Deployment

### Server Setup

1. **Create dedicated user:**
   ```bash
   sudo useradd -r -s /bin/false -d /opt/tg-fsyn tgbot
   sudo mkdir -p /opt/tg-fsyn
   sudo chown tgbot:tgbot /opt/tg-fsyn
   ```

2. **Deploy application:**
   ```bash
   # Build binary
   CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .
   
   # Copy files to server
   sudo cp main /opt/tg-fsyn/
   sudo cp .env /opt/tg-fsyn/
   sudo mkdir -p /opt/tg-fsyn/files
   sudo chown -R tgbot:tgbot /opt/tg-fsyn
   sudo chmod +x /opt/tg-fsyn/main
   ```

### Systemd Service

1. **Install service:**
   ```bash
   sudo cp tg-fsyn.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable tg-fsyn
   ```

2. **Manage service:**
   ```bash
   # Start service
   sudo systemctl start tg-fsyn
   
   # Check status
   sudo systemctl status tg-fsyn
   
   # View logs
   sudo journalctl -u tg-fsyn -f
   
   # Restart service
   sudo systemctl restart tg-fsyn
   
   # Stop service
   sudo systemctl stop tg-fsyn
   ```

### Production Configuration

Create production environment file:

```bash
# /opt/tg-fsyn/.env
TELEGRAM_BOT_TOKEN=your_production_bot_token
STORAGE_PATH=/opt/tg-fsyn/files
LOG_LEVEL=info
MAX_FILE_SIZE=52428800
BOT_DEBUG=false
```

### Security Considerations

1. **File Permissions:**
   ```bash
   sudo chmod 600 /opt/tg-fsyn/.env
   sudo chmod 755 /opt/tg-fsyn/main
   sudo chmod 755 /opt/tg-fsyn/files
   ```

2. **Firewall (if needed):**
   ```bash
   # Allow outbound HTTPS (for Telegram API)
   sudo ufw allow out 443
   ```

3. **Log Rotation:**
   ```bash
   # Create logrotate configuration
   sudo tee /etc/logrotate.d/tg-fsyn << EOF
   /var/log/tg-fsyn/*.log {
       daily
       missingok
       rotate 30
       compress
       delaycompress
       notifempty
       create 644 tgbot tgbot
   }
   EOF
   ```

## Cloud Deployment

### Docker on Cloud Platforms

#### AWS EC2

```bash
# Install Docker
sudo yum update -y
sudo amazon-linux-extras install docker
sudo service docker start
sudo usermod -a -G docker ec2-user

# Deploy with Docker Compose
git clone <repository-url>
cd tg-fsyn
cp .env.example .env
nano .env  # Add your bot token
docker-compose up -d
```

#### Google Cloud Platform

```bash
# Create VM and install Docker
gcloud compute instances create tg-bot-vm \
    --machine-type=e2-micro \
    --image-family=ubuntu-2004-lts \
    --image-project=ubuntu-os-cloud

# SSH and setup
gcloud compute ssh tg-bot-vm
sudo apt update
sudo apt install -y docker.io docker-compose
sudo usermod -a -G docker $USER

# Deploy
git clone <repository-url>
cd tg-fsyn
cp .env.example .env
nano .env
sudo docker-compose up -d
```

#### DigitalOcean Droplet

```bash
# Create droplet with Docker pre-installed
# Or install Docker on Ubuntu droplet:
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
sudo usermod -aG docker $USER

# Deploy
git clone <repository-url>
cd tg-fsyn
cp .env.example .env
nano .env
docker-compose up -d
```

### Kubernetes Deployment

Create Kubernetes manifests:

1. **ConfigMap:**
   ```yaml
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: tg-bot-config
   data:
     STORAGE_PATH: "/app/files"
     LOG_LEVEL: "info"
   ```

2. **Secret:**
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: tg-bot-secret
   type: Opaque
   stringData:
     TELEGRAM_BOT_TOKEN: "your_bot_token_here"
   ```

3. **Deployment:**
   ```yaml
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: tg-bot
   spec:
     replicas: 1
     selector:
       matchLabels:
         app: tg-bot
     template:
       metadata:
         labels:
           app: tg-bot
       spec:
         containers:
         - name: tg-bot
           image: tg-file-bot:latest
           envFrom:
           - configMapRef:
               name: tg-bot-config
           - secretRef:
               name: tg-bot-secret
           volumeMounts:
           - name: storage
             mountPath: /app/files
         volumes:
         - name: storage
           persistentVolumeClaim:
             claimName: tg-bot-storage
   ```

## Monitoring and Maintenance

### Health Checks

The application includes health checks:

```bash
# Docker health check
docker inspect --format='{{.State.Health.Status}}' tg-file-bot

# Manual process check
pgrep main || echo "Bot is not running"
```

### Monitoring Scripts

Create monitoring script:

```bash
#!/bin/bash
# /usr/local/bin/tg-bot-monitor.sh

CONTAINER_NAME="tg-file-bot"
SERVICE_NAME="tg-fsyn"

# Check Docker container
if docker ps | grep -q "$CONTAINER_NAME"; then
    echo "✅ Docker container is running"
else
    echo "❌ Docker container is not running"
    # Restart container
    docker-compose -f /path/to/docker-compose.yml restart
fi

# Check systemd service
if systemctl is-active --quiet "$SERVICE_NAME"; then
    echo "✅ Systemd service is running"
else
    echo "❌ Systemd service is not running"
    # Restart service
    systemctl restart "$SERVICE_NAME"
fi

# Check disk space
STORAGE_PATH="/opt/tg-fsyn/files"
USAGE=$(df "$STORAGE_PATH" | awk 'NR==2 {print $5}' | sed 's/%//')
if [ "$USAGE" -gt 80 ]; then
    echo "⚠️  Disk usage is high: ${USAGE}%"
fi
```

### Log Management

```bash
# View application logs
sudo journalctl -u tg-fsyn -f

# Docker logs
docker-compose logs -f

# Rotate logs
sudo logrotate -f /etc/logrotate.d/tg-fsyn
```

### Backup Strategy

```bash
#!/bin/bash
# Backup script for stored files

BACKUP_DATE=$(date +%Y%m%d_%H%M%S)
SOURCE_DIR="/opt/tg-fsyn/files"
BACKUP_DIR="/backup/tg-fsyn"

# Create backup
mkdir -p "$BACKUP_DIR"
tar -czf "$BACKUP_DIR/files_$BACKUP_DATE.tar.gz" -C "$SOURCE_DIR" .

# Keep only last 7 days of backups
find "$BACKUP_DIR" -name "files_*.tar.gz" -mtime +7 -delete

echo "Backup completed: files_$BACKUP_DATE.tar.gz"
```

## Troubleshooting

### Common Issues

1. **Bot not responding:**
   ```bash
   # Check if bot is running
   ./start.sh status
   
   # Check logs
   ./start.sh logs
   
   # Restart bot
   ./start.sh restart
   ```

2. **Permission errors:**
   ```bash
   # Fix file permissions
   sudo chown -R tgbot:tgbot /opt/tg-fsyn
   chmod 755 /opt/tg-fsyn/files
   ```

3. **Out of disk space:**
   ```bash
   # Check disk usage
   df -h
   
   # Clean old files if needed
   find /opt/tg-fsyn/files -type f -mtime +30 -delete
   ```

4. **Network connectivity:**
   ```bash
   # Test Telegram API connectivity
   curl -s https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getMe
   ```

### Debug Mode

Enable debug mode for troubleshooting:

```bash
# Set in environment file
BOT_DEBUG=true

# Or export directly
export BOT_DEBUG=true
```

### Performance Monitoring

```bash
# Monitor resource usage
top -p $(pgrep main)

# Monitor disk I/O
iostat -x 1

# Monitor network
netstat -an | grep :443
```

## Security Best Practices

1. **Environment Variables:**
   - Never commit `.env` files
   - Use secure methods to distribute tokens
   - Rotate tokens regularly

3. **File Storage:**
   - Set appropriate file permissions
   - Monitor disk usage
   - Implement file retention policies
   - All files stored in single directory with timestamp naming

3. **Network Security:**
   - Use HTTPS only
   - Implement rate limiting if needed
   - Monitor for suspicious activity

4. **Updates:**
   - Keep dependencies updated
   - Monitor for security advisories
   - Test updates in staging first

## Performance Tuning

1. **Resource Limits:**
   ```bash
   # Set in systemd service
   MemoryHigh=512M
   MemoryMax=1G
   LimitNOFILE=65536
   ```

2. **File Storage:**
   - Use fast storage (SSD)
   - Consider separate partition for files
   - Implement file compression if needed

3. **Monitoring:**
   - Set up alerting for high resource usage
   - Monitor response times
   - Track file storage growth

---

For additional support, please refer to the main README.md file or open an issue in the project repository.