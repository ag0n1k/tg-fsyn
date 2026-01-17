# Telegram File Storage Bot

A simple yet powerful Telegram bot built with Go that receives files from users and stores them locally. Perfect for creating a personal file storage system accessible through Telegram.

## Features

- 📁 **Multiple File Types Support**: Documents, photos, videos, audio, voice messages, video notes, and stickers
- 📁 **Simple Storage**: All files stored in a single directory with timestamp naming
- 🔒 **Access Control**: Restrict bot access to authorized users only
- 👨‍💼 **Admin Features**: Admin commands for user management
- 🔐 **Security**: Files are stored securely with proper permissions
- 📦 **Docker Support**: Easy deployment with Docker and Docker Compose
- 🚀 **Lightweight**: Minimal resource usage with Alpine Linux base
- 📝 **Detailed Logging**: Comprehensive logging for monitoring and debugging
- 💾 **Size Limits**: Configurable file size limits (default: 50MB)
- 📊 **Synology Status Monitoring**: Monitor download tasks and receive notifications for status changes

## Supported File Types

- **Documents**: PDF, DOC, TXT, ZIP, etc.
- **Photos**: JPG, PNG, GIF, etc.
- **Videos**: MP4, AVI, MOV, etc.
- **Audio**: MP3, WAV, FLAC, etc.
- **Voice Messages**: OGG format
- **Video Notes**: Circular videos from Telegram
- **Stickers**: WEBP format

## Quick Start

### Prerequisites

- Go 1.21 or higher (for local development)
- Docker and Docker Compose (for containerized deployment)
- A Telegram Bot Token (obtain from [@BotFather](https://t.me/BotFather))

### Getting Your Bot Token

1. Open Telegram and search for [@BotFather](https://t.me/BotFather)
2. Send `/newbot` command
3. Follow the instructions to create your bot
4. Save the token provided by BotFather

### Option 1: Docker Deployment (Recommended)

1. **Clone the repository:**
   ```bash
   git clone <repository-url>
   cd tg-fsyn
   ```

2. **Set up environment variables:**
   ```bash
   cp .env.example .env
   nano .env  # Edit with your bot token
   ```

3. **Build and run with Docker Compose:**
   ```bash
   docker-compose up -d
   ```

4. **Check logs:**
   ```bash
   docker-compose logs -f
   ```

### Option 1b: Synology NAS Deployment

For Synology NAS users, use the automated setup script:

1. **Quick Setup:**
   ```bash
   ./setup-synology.sh --token YOUR_BOT_TOKEN --user YOUR_USER_ID
   ```

2. **Or step-by-step:**
   ```bash
   # Fix permissions for Synology (UID 1026)
   make fix-permissions
   
   # Deploy with Synology-specific settings
   make synology-deploy
   
   # Check status
   make synology-status
   ```

3. **Alternative (if permissions are problematic):**
   ```bash
   # Use root mode (less secure but simpler)
   ./setup-synology.sh --root-mode
   ```

### Option 2: Local Development

1. **Clone and setup:**
   ```bash
   git clone <repository-url>
   cd tg-fsyn
   go mod tidy
   ```

2. **Set environment variables:**
   ```bash
   export TELEGRAM_BOT_TOKEN="your_bot_token_here"
   export STORAGE_PATH="./files"
   ```

3. **Run the bot:**
   ```bash
   go run main.go
   ```

## Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `TELEGRAM_BOT_TOKEN` | Your Telegram bot token | - | ✅ |
| `ALLOWED_USERS` | Comma-separated list of allowed user IDs | - | ❌ |
| `ADMIN_USERS` | Comma-separated list of admin user IDs | - | ❌ |
| `STORAGE_PATH` | Directory to store files | `./files` | ❌ |
| `LOG_LEVEL` | Logging level | `info` | ❌ |
| `MAX_FILE_SIZE` | Maximum file size in bytes | `52428800` (50MB) | ❌ |
| `BOT_DEBUG` | Enable debug mode | `false` | ❌ |
| `SYNOLOGY_HOST` | Synology DSM IP address | `127.0.0.1` | ❌ |
| `SYNOLOGY_PORT` | Synology DSM port | `5000` | ❌ |
| `SYNOLOGY_USERNAME` | Synology username | (empty) | ❌ |
| `SYNOLOGY_PASSWORD` | Synology password | (empty) | ❌ |

**Note for Synology Users:** The bot uses UID `1026` and GID `100` by default, which matches standard Synology user permissions.

### File Organization

Files are stored directly in the storage directory:
```
files/
├── document_1641234567_ABC123.pdf
├── photo_1641234568_DEF456.jpg
├── video_1641234569_GHI789.mp4
└── audio_1641234570_JKL012.mp3
```

Files are named with timestamps and file IDs for easy identification.

## Docker Commands

### Building the Image
```bash
docker build -t tg-file-bot .
```

### Running with Docker
```bash
docker run -d \
  --name tg-file-bot \
  -e TELEGRAM_BOT_TOKEN="your_token_here" \
  -v $(pwd)/files:/app/files \
  tg-file-bot
```

### Using Docker Compose
```bash
# Start the bot
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the bot
docker-compose down

# Rebuild and restart
docker-compose up -d --build
```

## Synology NAS Setup

### Prerequisites for Synology
- Synology NAS with DSM 6.0 or higher
- Docker package installed from Package Center
- SSH access enabled (for advanced setup)

### Quick Setup
```bash
# Automated setup script
./setup-synology.sh --token YOUR_BOT_TOKEN --user YOUR_USER_ID

# Or interactive setup
./setup-synology.sh
```

### Manual Synology Setup

1. **Create storage directory:**
   ```bash
   mkdir -p ./files
   sudo chown 1026:100 ./files
   chmod 755 ./files
   ```

2. **Use Synology-specific compose file:**
   ```bash
   # Copy environment template
   cp .env.example .env
   
   # Edit with your settings
   nano .env
   
   # Deploy with Synology settings
   docker-compose -f docker-compose.synology.yml up -d --build
   ```

3. **Alternative - Root mode (if permissions fail):**
   ```bash
   docker-compose --profile root-mode up -d --build
   ```

### Synology Troubleshooting

**Permission denied errors:**
```bash
# Fix permissions automatically
make fix-permissions

# Or manually
sudo chown -R 1026:100 ./files
chmod -R 755 ./files
```

**Container won't start:**
```bash
# Check logs
make synology-logs

# Try root mode
make setup-synology-root
```

## Access Control

### User Authorization

The bot supports user access control to restrict usage to authorized users only:

1. **Get your Telegram User ID:**
   - Send `/id` command to the bot
   - The bot will reply with your user ID

2. **Configure allowed users:**
   ```bash
   # In .env file
   ALLOWED_USERS=123456789,987654321,555666777
   
   # Or as environment variable
   export ALLOWED_USERS="123456789,987654321,555666777"
   ```

3. **Configure admin users (optional):**
   ```bash
   # In .env file
   ADMIN_USERS=123456789
   
   # Or as environment variable
   export ADMIN_USERS="123456789"
   ```

### Admin Features

Admins have additional capabilities:
- Add/remove users from the allowed list
- View bot statistics
- List all authorized users

## Bot Commands

### Regular Commands
- `/start` - Show welcome message and bot capabilities
- `/help` - Display help information and supported file types
- `/id` - Get your Telegram user ID (useful for access control setup)
- `/status` - Show current download status from Synology

### Admin Commands (Admin users only)
- `/admin list` - List all allowed users
- `/admin add <user_id>` - Add user to allowed list
- `/admin remove <user_id>` - Remove user from allowed list
- `/admin status` - Show bot statistics

## Usage

### Initial Setup
1. Get your bot token from [@BotFather](https://t.me/BotFather)
2. Find your Telegram user ID:
   - Start a chat with your bot
   - Send `/id` command
   - Save the user ID for configuration

### Configure Access (Optional but Recommended)
```bash
# Add your user ID to allowed users
export ALLOWED_USERS="your_user_id_here"

# Optionally, make yourself an admin
export ADMIN_USERS="your_user_id_here"
```

### Using the Bot
1. Start a chat with your bot
2. Send `/start` to see the welcome message
3. Send any supported file type
4. The bot will confirm successful storage
5. Files are saved to the configured storage path

### Getting Other Users' Access
1. Users should send `/id` to the bot to get their user ID
2. Admins can add them using `/admin add <user_id>`
3. Or add their ID to the `ALLOWED_USERS` environment variable

## Development

### Project Structure

```
tg-fsyn/
├── main.go              # Main application file
├── go.mod              # Go module dependencies
├── go.sum              # Dependency checksums
├── Dockerfile          # Docker build instructions
├── docker-compose.yml  # Docker Compose configuration
├── .env.example        # Environment variables template
├── .dockerignore       # Docker ignore patterns
└── README.md          # This file
```

### Adding New Features

The bot is structured with clear separation of concerns:

- `Bot` struct handles the main bot logic
- Individual handler functions for each file type
- Utility functions for file operations
- Environment-based configuration

To add support for new file types, create a new handler function following the existing pattern.

### Security Considerations

- **Access Control**: Configurable user authorization prevents unauthorized access
- **Simple Structure**: All files stored in a single directory with clear naming convention
- **Admin Controls**: Admin users can manage access without server access
- **Secure Container**: The application runs as a non-root user in Docker
- **File Limits**: File size limits prevent abuse
- **Input Validation**: Proper validation on all file operations
- **Privacy**: No sensitive information is logged
- **User ID Logging**: Unauthorized access attempts are logged for security monitoring

## Troubleshooting

### Common Issues

1. **Bot not responding:**
   - Check if the bot token is correct
   - Verify the bot is not blocked by Telegram
   - Check if your user ID is in the allowed users list
   - Check application logs for errors

2. **Access Denied error:**
   - Get your user ID with `/id` command
   - Ask an admin to add you with `/admin add <your_user_id>`
   - Or add your user ID to `ALLOWED_USERS` environment variable

2. **Files not saving:**
   - Verify storage directory permissions
   - Check available disk space
   - Review file size limits

3. **Docker issues:**
   - Ensure Docker daemon is running
   - Check port conflicts
   - Verify volume mounts

### Logs

Check application logs for debugging:
```bash
# Docker Compose
docker-compose logs -f tg-bot

# Docker
docker logs tg-file-bot

# Local development
# Logs are printed to stdout
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

If you encounter any issues or have questions:

1. Check the troubleshooting section
2. Review the logs for error messages
3. Open an issue on GitHub
4. Join our community discussions

## Roadmap

- [x] User access control and authorization
- [x] Admin commands for user management
- [x] Synology status monitoring with notifications
- [ ] Web interface for file management
- [ ] Database integration for metadata
- [ ] Role-based permissions (viewer, uploader, admin)
- [ ] User quotas and storage limits
- [ ] File compression options
- [ ] Multi-language support
- [ ] File encryption
- [ ] File sharing capabilities
- [ ] Audit logs and activity tracking

---

Made with ❤️ using Go and the Telegram Bot API