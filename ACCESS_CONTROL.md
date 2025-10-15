# Access Control Setup Guide

This guide explains how to configure user access control for your Telegram File Storage Bot to restrict usage to authorized users only.

## Table of Contents

- [Overview](#overview)
- [Quick Setup](#quick-setup)
- [User Roles](#user-roles)
- [Configuration Methods](#configuration-methods)
- [Getting User IDs](#getting-user-ids)
- [Admin Commands](#admin-commands)
- [Security Best Practices](#security-best-practices)
- [Troubleshooting](#troubleshooting)

## Overview

The bot supports two types of access control:

- **Allowed Users**: Users who can use the bot to store files
- **Admin Users**: Users who can manage access and view bot statistics

Without configuration, the bot allows **all users** to access it. This is not recommended for production use.

## Quick Setup

### 1. Get Your User ID

Start a conversation with your bot and send:
```
/id
```

The bot will reply with your user ID (e.g., `123456789`).

### 2. Configure Access Control

Edit your `.env` file:
```bash
# Allow only specific users (comma-separated)
ALLOWED_USERS=123456789,987654321

# Set admin users (optional)
ADMIN_USERS=123456789
```

### 3. Restart the Bot

```bash
# Docker Compose
docker-compose restart

# Or using start script
./start.sh restart

# Or using Makefile
make compose-restart
```

## User Roles

### Regular Users (ALLOWED_USERS)

**Permissions:**
- Upload and store files
- Use basic commands (`/start`, `/help`, `/id`)

**Cannot:**
- Add/remove other users
- View bot statistics
- Access admin commands

### Admin Users (ADMIN_USERS)

**Permissions:**
- All regular user permissions
- Add users: `/admin add <user_id>`
- Remove users: `/admin remove <user_id>`
- List users: `/admin list`
- View statistics: `/admin status`

**Note:** Admin users are automatically included in the allowed users list.

## Configuration Methods

### Method 1: Environment File (.env)

Create or edit `.env` file:
```bash
# Required: Your bot token
TELEGRAM_BOT_TOKEN=your_bot_token_here

# Access Control
ALLOWED_USERS=123456789,987654321,555666777
ADMIN_USERS=123456789
```

### Method 2: Environment Variables

```bash
export TELEGRAM_BOT_TOKEN="your_bot_token_here"
export ALLOWED_USERS="123456789,987654321,555666777"
export ADMIN_USERS="123456789"
```

### Method 3: Docker Environment

In `docker-compose.yml`:
```yaml
services:
  tg-bot:
    environment:
      - TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN}
      - ALLOWED_USERS=123456789,987654321
      - ADMIN_USERS=123456789
```

## Getting User IDs

### Method 1: Bot Command (Recommended)

1. User sends `/id` to the bot
2. Bot replies with user ID
3. Admin adds the ID to configuration

### Method 2: Helper Script

Use the provided script:
```bash
# From project root
./scripts/get-user-id.sh --updates

# Or with custom token
./scripts/get-user-id.sh --token YOUR_BOT_TOKEN --updates
```

### Method 3: Bot Logs

Check bot logs for unauthorized access attempts:
```bash
# Docker Compose
docker-compose logs | grep "Unauthorized access"

# Example log entry:
# Unauthorized access attempt from user 123456789 (@username)
```

### Method 4: Using Makefile

```bash
# Get user IDs from recent interactions
make get-user-id

# Show current access configuration
make show-access-config

# Interactive setup
make setup-access
```

## Admin Commands

Admins can manage users through bot commands:

### List Users
```
/admin list
```
Shows all currently allowed users.

### Add User
```
/admin add 123456789
```
Adds user ID `123456789` to the allowed list.

### Remove User
```
/admin remove 123456789
```
Removes user ID `123456789` from the allowed list.

### Show Statistics
```
/admin status
```
Displays bot statistics and configuration.

### Help
```
/admin
```
Shows available admin commands.

## Security Best Practices

### 1. Always Configure Access Control

```bash
# Don't leave this empty in production
ALLOWED_USERS=
```

### 2. Limit Admin Users

Only add trusted users as admins:
```bash
# Good: One or few admins
ADMIN_USERS=123456789

# Avoid: Too many admins
ADMIN_USERS=123456789,987654321,555666777,444555666
```

### 3. Regular Access Review

Periodically review and clean up user access:
```bash
# List current users
/admin list

# Remove inactive users
/admin remove <user_id>
```

### 4. Monitor Access Attempts

Check logs for unauthorized access:
```bash
# View recent logs
docker-compose logs -f | grep -E "(Unauthorized|Admin.*added|Admin.*removed)"
```

### 5. Use Environment Files Safely

```bash
# Secure .env file permissions
chmod 600 .env

# Don't commit .env to version control
echo ".env" >> .gitignore
```

### 6. File Storage Security

```bash
# Files are stored directly in storage directory with timestamps
# Monitor storage usage and implement retention policies if needed
du -h /path/to/storage/files
```

## Troubleshooting

### Bot Says "Access Denied"

**Problem:** User gets "🚫 Access Denied" message.

**Solutions:**
1. Get user ID with `/id` command
2. Add to `ALLOWED_USERS`:
   ```bash
   ALLOWED_USERS=existing_users,new_user_id
   ```
3. Restart bot:
   ```bash
   docker-compose restart
   ```

### Admin Commands Not Working

**Problem:** `/admin` commands return "Access denied".

**Solutions:**
1. Verify your user ID is in `ADMIN_USERS`:
   ```bash
   ADMIN_USERS=your_user_id
   ```
2. Restart bot after configuration change
3. Check configuration:
   ```bash
   make show-access-config
   ```

### Can't Get User IDs

**Problem:** Helper script or `/id` command not working.

**Solutions:**
1. Verify bot token is correct
2. Ensure bot is running
3. Check network connectivity
4. Try alternative methods:
   ```bash
   # Check bot logs
   docker-compose logs | grep "from user"
   
   # Manual API call
   curl "https://api.telegram.org/bot$TOKEN/getUpdates"
   ```

### Configuration Not Applied

**Problem:** Changes to `.env` not taking effect.

**Solutions:**
1. Restart the bot completely:
   ```bash
   docker-compose down
   docker-compose up -d
   ```
2. Verify file format:
   ```bash
   # Correct format
   ALLOWED_USERS=123456789,987654321
   
   # Wrong format (no spaces, quotes)
   ALLOWED_USERS = "123456789, 987654321"
   ```
3. Check for file encoding issues:
   ```bash
   file .env  # Should show ASCII text
   ```

### Bot Allows Everyone

**Problem:** Bot doesn't restrict access despite configuration.

**Solutions:**
1. Check if `ALLOWED_USERS` is empty (allows everyone by default)
2. Verify environment variable loading:
   ```bash
   # In container
   docker-compose exec tg-bot env | grep ALLOWED
   ```
3. Check for configuration file issues:
   ```bash
   # Validate .env syntax
   grep -E "^[A-Z_]+=" .env
   ```

## Example Configurations

### Single User Setup
```bash
TELEGRAM_BOT_TOKEN=your_token_here
ALLOWED_USERS=123456789
ADMIN_USERS=123456789
```

### Family/Team Setup
```bash
TELEGRAM_BOT_TOKEN=your_token_here
ALLOWED_USERS=123456789,987654321,555666777
ADMIN_USERS=123456789,987654321
```

### Organization Setup
```bash
TELEGRAM_BOT_TOKEN=your_token_here
ALLOWED_USERS=123456789,987654321,555666777,444333222,111222333
ADMIN_USERS=123456789  # Only one main admin
```

## Migration from Open Access

If your bot currently allows everyone and you want to add restrictions:

1. **Identify current users:**
   ```bash
   # Check recent interactions
   ./scripts/get-user-id.sh --updates
   
   # Check logs for user activity
   docker-compose logs | grep -o "User: [0-9]*" | sort -u
   ```

2. **Gradually implement restrictions:**
   ```bash
   # Start with all current users
   ALLOWED_USERS=user1,user2,user3,user4
   ```

3. **Announce the change:**
   - Inform users about the new access control
   - Provide instructions for getting user IDs
   - Set a transition period

4. **Monitor and adjust:**
   - Check for access denied messages
   - Add legitimate users as needed
   - Remove inactive users over time

## Support

If you need help with access control setup:

1. Check this guide and the main README.md
2. Use the troubleshooting section above
3. Review bot logs for specific error messages
4. Test configuration with known user IDs
5. Open an issue in the project repository

---

**Remember:** Access control is crucial for security. Always configure it in production environments and review access regularly.