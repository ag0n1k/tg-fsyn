package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

const (
	DefaultStoragePath     = "./files"
	MaxFileSize            = 50 * 1024 * 1024 // 50MB
	StatusUpdateInterval   = 5 * time.Minute
)

// Task represents a download task
type Task struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Size       int64  `json:"size"`
	Type       string `json:"type"`
	Username   string `json:"username"`
	Additional struct {
		Detail struct {
			CompletedTime int64 `json:"completed_time"`
			StartedTime   int64 `json:"started_time"`
		} `json:"detail"`
		File []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"file"`
	} `json:"additional"`
}

type Bot struct {
	api           *tgbotapi.BotAPI
	storagePath   string
	allowedUsers  map[int64]bool
	adminUsers    map[int64]bool
	statusService *StatusService
}

func NewBot(token, storagePath string, allowedUsers, adminUsers []int64) (*Bot, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	// Create storage directory if it doesn't exist
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Convert slices to maps for faster lookups
	userMap := make(map[int64]bool)
	for _, userID := range allowedUsers {
		userMap[userID] = true
	}

	adminMap := make(map[int64]bool)
	for _, userID := range adminUsers {
		adminMap[userID] = true
	}

	// Build Synology client from environment
	host := os.Getenv("SYNOLOGY_HOST")
	if host == "" {
		host = "192.168.1.34"
	}

	port := os.Getenv("SYNOLOGY_PORT")
	if port == "" {
		port = "5000"
	}

	username := os.Getenv("SYNOLOGY_USERNAME")
	if username == "" {
		log.Fatal("SYNOLOGY_USERNAME environment variable is required")
	}

	password := os.Getenv("SYNOLOGY_PASSWORD")
	if password == "" {
		log.Fatal("SYNOLOGY_PASSWORD environment variable is required")
	}

	synClient := NewSynologyHTTPClient(host, port, username, password)
	statusSvc := NewStatusService(synClient, adminMap, bot, StatusUpdateInterval)

	return &Bot{
		api:           bot,
		storagePath:   storagePath,
		allowedUsers:  userMap,
		adminUsers:    adminMap,
		statusService: statusSvc,
	}, nil
}

func (b *Bot) Start() {
	b.api.Debug = false

	log.Printf("Authorized on account %s", b.api.Self.UserName)

	// Start the status monitoring service
	if b.statusService != nil {
		b.statusService.Start()
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			b.handleMessage(update.Message)
		}
	}
}

func (b *Bot) handleMessage(message *tgbotapi.Message) {
	chatID := message.Chat.ID
	userID := message.From.ID

	// Check if user is authorized
	if !b.isUserAllowed(userID) {
		log.Printf("Unauthorized access attempt from user %d (%s)", userID, message.From.UserName)
		b.sendUnauthorizedMessage(chatID)
		return
	}

	// Handle different types of content
	switch {
	case message.Document != nil:
		b.handleDocument(message.Document, chatID, message.MessageID)
	case message.Photo != nil && len(message.Photo) > 0:
		// Get the largest photo
		photo := message.Photo[len(message.Photo)-1]
		b.handlePhoto(&photo, chatID, message.MessageID)
	case message.Video != nil:
		b.handleVideo(message.Video, chatID, message.MessageID)
	case message.Audio != nil:
		b.handleAudio(message.Audio, chatID, message.MessageID)
	case message.Voice != nil:
		b.handleVoice(message.Voice, chatID, message.MessageID)
	case message.VideoNote != nil:
		b.handleVideoNote(message.VideoNote, chatID, message.MessageID)
	case message.Sticker != nil:
		b.handleSticker(message.Sticker, chatID, message.MessageID)
	case message.Text == "/start":
		b.sendWelcomeMessage(chatID)
	case message.Text == "/help":
		b.sendHelpMessage(chatID)
	case message.Text == "/id":
		b.sendUserIDMessage(chatID, userID, message.From)
	case message.Text == "/status":
		b.handleStatusCommand(chatID)
	case strings.HasPrefix(message.Text, "/admin"):
		b.handleAdminCommand(message, chatID, userID)
	case message.Text != "":
		b.sendTextMessage(chatID, "Please send me a file, photo, video, or audio to store.")
	default:
		b.sendTextMessage(chatID, "Unsupported message type. Please send me a file.")
	}
}

func (b *Bot) handleDocument(document *tgbotapi.Document, chatID int64, messageID int) {
	if document.FileSize > MaxFileSize {
		b.sendTextMessage(chatID, fmt.Sprintf("File too large. Maximum size is %d MB", MaxFileSize/(1024*1024)))
		return
	}

	fileName := document.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("document_%d_%s", time.Now().Unix(), document.FileID)
	}

	if err := b.downloadAndSave(document.FileID, fileName, chatID); err != nil {
		log.Printf("Error handling document: %v", err)
		b.sendTextMessage(chatID, "Failed to save the document.")
		return
	}

	// Force status update when file is received
	b.forceStatusUpdate(chatID)

	b.sendTextMessage(chatID, fmt.Sprintf("✅ '%s'", fileName))
}

func (b *Bot) handlePhoto(photo *tgbotapi.PhotoSize, chatID int64, messageID int) {
	fileName := fmt.Sprintf("photo_%d_%s.jpg", time.Now().Unix(), photo.FileID)

	if err := b.downloadAndSave(photo.FileID, fileName, chatID); err != nil {
		log.Printf("Error handling photo: %v", err)
		b.sendTextMessage(chatID, "Failed to save the photo.")
		return
	}

	// Force status update when file is received
	b.forceStatusUpdate(chatID)

	b.sendTextMessage(chatID, fmt.Sprintf("✅ Photo '%s' saved successfully!", fileName))
}

func (b *Bot) handleVideo(video *tgbotapi.Video, chatID int64, messageID int) {
	if video.FileSize > MaxFileSize {
		b.sendTextMessage(chatID, fmt.Sprintf("Video too large. Maximum size is %d MB", MaxFileSize/(1024*1024)))
		return
	}

	fileName := fmt.Sprintf("video_%d_%s.mp4", time.Now().Unix(), video.FileID)

	if err := b.downloadAndSave(video.FileID, fileName, chatID); err != nil {
		log.Printf("Error handling video: %v", err)
		b.sendTextMessage(chatID, "Failed to save the video.")
		return
	}

	// Force status update when file is received
	b.forceStatusUpdate(chatID)

	b.sendTextMessage(chatID, fmt.Sprintf("✅ Video '%s' saved successfully!", fileName))
}

func (b *Bot) handleAudio(audio *tgbotapi.Audio, chatID int64, messageID int) {
	if audio.FileSize > MaxFileSize {
		b.sendTextMessage(chatID, fmt.Sprintf("Audio too large. Maximum size is %d MB", MaxFileSize/(1024*1024)))
		return
	}

	fileName := audio.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("audio_%d_%s.mp3", time.Now().Unix(), audio.FileID)
	}

	if err := b.downloadAndSave(audio.FileID, fileName, chatID); err != nil {
		log.Printf("Error handling audio: %v", err)
		b.sendTextMessage(chatID, "Failed to save the audio.")
		return
	}

	// Force status update when file is received
	b.forceStatusUpdate(chatID)

	b.sendTextMessage(chatID, fmt.Sprintf("✅ Audio '%s' saved successfully!", fileName))
}

func (b *Bot) handleVoice(voice *tgbotapi.Voice, chatID int64, messageID int) {
	fileName := fmt.Sprintf("voice_%d_%s.ogg", time.Now().Unix(), voice.FileID)

	if err := b.downloadAndSave(voice.FileID, fileName, chatID); err != nil {
		log.Printf("Error handling voice: %v", err)
		b.sendTextMessage(chatID, "Failed to save the voice message.")
		return
	}

	// Force status update when file is received
	b.forceStatusUpdate(chatID)

	b.sendTextMessage(chatID, fmt.Sprintf("✅ Voice message '%s' saved successfully!", fileName))
}

func (b *Bot) handleVideoNote(videoNote *tgbotapi.VideoNote, chatID int64, messageID int) {
	fileName := fmt.Sprintf("videonote_%d_%s.mp4", time.Now().Unix(), videoNote.FileID)

	if err := b.downloadAndSave(videoNote.FileID, fileName, chatID); err != nil {
		log.Printf("Error handling video note: %v", err)
		b.sendTextMessage(chatID, "Failed to save the video note.")
		return
	}

	// Force status update when file is received
	b.forceStatusUpdate(chatID)

	b.sendTextMessage(chatID, fmt.Sprintf("✅ Video note '%s' saved successfully!", fileName))
}

func (b *Bot) handleSticker(sticker *tgbotapi.Sticker, chatID int64, messageID int) {
	fileName := fmt.Sprintf("sticker_%d_%s.webp", time.Now().Unix(), sticker.FileID)

	if err := b.downloadAndSave(sticker.FileID, fileName, chatID); err != nil {
		log.Printf("Error handling sticker: %v", err)
		b.sendTextMessage(chatID, "Failed to save the sticker.")
		return
	}

	// Force status update when file is received
	b.forceStatusUpdate(chatID)

	b.sendTextMessage(chatID, fmt.Sprintf("✅ Sticker '%s' saved successfully!", fileName))
}

func (b *Bot) downloadAndSave(fileID, fileName string, chatID int64) error {
	// Get file info from Telegram
	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Download file from Telegram
	fileURL := file.Link(b.api.Token)
	resp, err := http.Get(fileURL)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	// Create local file directly in storage path
	filePath := filepath.Join(b.storagePath, fileName)
	localFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	// Copy content
	_, err = io.Copy(localFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save file content: %w", err)
	}

	log.Printf("File saved: %s from user %d", filePath, chatID)
	return nil
}

func (b *Bot) sendWelcomeMessage(chatID int64) {
	message := `🤖 Welcome to File Storage Bot!

I can help you store various types of files:
• Documents (PDF, DOC, TXT, etc.)
• Photos and Images
• Videos
• Audio files
• Voice messages
• Video notes
• Stickers

Just send me any file and I'll store it safely for you!

Use /help to see available commands.
Use /id to get your Telegram user ID.`

	b.sendTextMessage(chatID, message)
}

func (b *Bot) sendHelpMessage(chatID int64) {
	message := `📖 Available Commands:

/start - Show welcome message
/help - Show this help message
/id - Show your Telegram user ID`

	// Add admin commands if user is admin
	if b.isUserAdmin(chatID) {
		message += `
/admin - Admin commands (list, add, remove users)`
	}

	message += `

📁 Supported File Types:
• Documents: Any file type (max 50MB)
• Photos: JPG, PNG, etc.
• Videos: MP4, AVI, etc. (max 50MB)
• Audio: MP3, WAV, etc. (max 50MB)
• Voice messages: OGG format
• Video notes: Circular videos
• Stickers: WEBP format

Files are stored with timestamps and file IDs for easy identification.`

	b.sendTextMessage(chatID, message)
}

func (b *Bot) sendUserIDMessage(chatID int64, userID int64, user *tgbotapi.User) {
	username := user.UserName
	firstName := user.FirstName
	lastName := user.LastName

	var nameInfo string
	if username != "" {
		nameInfo = fmt.Sprintf("@%s", username)
	} else {
		nameInfo = firstName
		if lastName != "" {
			nameInfo += " " + lastName
		}
	}

	message := fmt.Sprintf(`🆔 Your Telegram User Information:

👤 Name: %s
🔢 User ID: %d

This ID can be used by bot administrators to grant you access to restricted bots.`, nameInfo, userID)

	b.sendTextMessage(chatID, message)
}

func (b *Bot) sendTextMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("Failed to send message: %v", err)
	}
}

func (b *Bot) isUserAdmin(userID int64) bool {
	return b.adminUsers[userID]
}

func (b *Bot) handleAdminCommand(message *tgbotapi.Message, chatID int64, userID int64) {
	if !b.isUserAdmin(userID) {
		b.sendTextMessage(chatID, "🚫 Access denied. Admin privileges required.")
		return
	}

	parts := strings.Fields(message.Text)
	if len(parts) < 2 {
		b.sendAdminHelp(chatID)
		return
	}

	command := parts[1]
	switch command {
	case "list":
		b.handleAdminListUsers(chatID)
	case "add":
		if len(parts) < 3 {
			b.sendTextMessage(chatID, "Usage: /admin add <user_id>")
			return
		}
		b.handleAdminAddUser(chatID, parts[2])
	case "remove":
		if len(parts) < 3 {
			b.sendTextMessage(chatID, "Usage: /admin remove <user_id>")
			return
		}
		b.handleAdminRemoveUser(chatID, parts[2])
	case "status":
		b.handleAdminStatus(chatID)
	default:
		b.sendAdminHelp(chatID)
	}
}

func (b *Bot) sendAdminHelp(chatID int64) {
	message := `🔧 Admin Commands:

/admin list - List all allowed users
/admin add <user_id> - Add user to allowed list
/admin remove <user_id> - Remove user from allowed list
/admin status - Show bot statistics

Example: /admin add 123456789`

	b.sendTextMessage(chatID, message)
}

func (b *Bot) handleAdminListUsers(chatID int64) {
	if len(b.allowedUsers) == 0 {
		b.sendTextMessage(chatID, "📝 No user restrictions configured. All users can access the bot.")
		return
	}

	var userList []string
	for userID := range b.allowedUsers {
		userList = append(userList, strconv.FormatInt(userID, 10))
	}

	message := fmt.Sprintf("👥 Allowed Users (%d total):\n\n%s", len(userList), strings.Join(userList, "\n"))
	b.sendTextMessage(chatID, message)
}

func (b *Bot) handleAdminAddUser(chatID int64, userIDStr string) {
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		b.sendTextMessage(chatID, "❌ Invalid user ID format")
		return
	}

	if b.allowedUsers[userID] {
		b.sendTextMessage(chatID, fmt.Sprintf("ℹ️ User %d is already in the allowed list", userID))
		return
	}

	b.allowedUsers[userID] = true
	b.sendTextMessage(chatID, fmt.Sprintf("✅ User %d added to allowed list", userID))
	log.Printf("Admin %d added user %d to allowed list", chatID, userID)
}

func (b *Bot) handleAdminRemoveUser(chatID int64, userIDStr string) {
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		b.sendTextMessage(chatID, "❌ Invalid user ID format")
		return
	}

	if !b.allowedUsers[userID] {
		b.sendTextMessage(chatID, fmt.Sprintf("ℹ️ User %d is not in the allowed list", userID))
		return
	}

	delete(b.allowedUsers, userID)
	b.sendTextMessage(chatID, fmt.Sprintf("✅ User %d removed from allowed list", userID))
	log.Printf("Admin %d removed user %d from allowed list", chatID, userID)
}

func (b *Bot) handleStatusCommand(chatID int64) {
	if b.statusService == nil {
		b.sendTextMessage(chatID, "⚠️ Status service not initialized")
		return
	}

	status, _ := b.statusService.GetStatus()
	if len(status) == 0 {
		b.sendTextMessage(chatID, "No download tasks found.")
		return
	}

	message := b.statusService.FormatStatusMessage()
	b.sendTextMessage(chatID, message)
}

// forceStatusUpdate forces an immediate status update
func (b *Bot) forceStatusUpdate(chatID int64) {
	if b.statusService == nil {
		return
	}

	// Trigger immediate status check
	b.statusService.checkStatus()

	// Send current status to user
	status, _ := b.statusService.GetStatus()
	if len(status) == 0 {
		b.sendTextMessage(chatID, "No download tasks found.")
		return
	}

	message := b.statusService.FormatStatusMessage()
	b.sendTextMessage(chatID, message)
}

func (b *Bot) handleAdminStatus(chatID int64) {
	allowedCount := len(b.allowedUsers)
	adminCount := len(b.adminUsers)

	message := fmt.Sprintf(`📊 Bot Status:

👥 Allowed Users: %d
🔧 Admin Users: %d
📁 Storage Path: %s
🤖 Bot Username: @%s

Memory: Runtime statistics available via process monitoring`, allowedCount, adminCount, b.storagePath, b.api.Self.UserName)

	b.sendTextMessage(chatID, message)
}

func (b *Bot) isUserAllowed(userID int64) bool {
	if len(b.allowedUsers) == 0 {
		// If no users are configured, allow everyone (backward compatibility)
		return true
	}
	return b.allowedUsers[userID]
}

func (b *Bot) sendUnauthorizedMessage(chatID int64) {
	message := `🚫 Access Denied

Sorry, you are not authorized to use this bot.

If you believe this is an error, please contact the bot administrator.`

	b.sendTextMessage(chatID, message)
}

func parseAllowedUsers(allowedUsersStr string) []int64 {
	if allowedUsersStr == "" {
		return []int64{}
	}

	var users []int64
	userStrings := strings.Split(allowedUsersStr, ",")

	for _, userStr := range userStrings {
		userStr = strings.TrimSpace(userStr)
		if userStr == "" {
			continue
		}

		userID, err := strconv.ParseInt(userStr, 10, 64)
		if err != nil {
			log.Printf("Warning: Invalid user ID '%s' in ALLOWED_USERS: %v", userStr, err)
			continue
		}

		users = append(users, userID)
	}

	return users
}

func main() {
	// Load .env file if it exists
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using environment variables directly")
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	storagePath := os.Getenv("STORAGE_PATH")
	if storagePath == "" {
		storagePath = DefaultStoragePath
	}

	// Parse allowed users list
	allowedUsersStr := os.Getenv("ALLOWED_USERS")
	allowedUsers := parseAllowedUsers(allowedUsersStr)

	// Parse admin users list
	adminUsersStr := os.Getenv("ADMIN_USERS")
	adminUsers := parseAllowedUsers(adminUsersStr)

	if len(allowedUsers) > 0 {
		log.Printf("Bot access restricted to %d users: %v", len(allowedUsers), allowedUsers)
	} else {
		log.Printf("Warning: No user restrictions configured. Bot is accessible to all users.")
	}

	if len(adminUsers) > 0 {
		log.Printf("Bot has %d admin users: %v", len(adminUsers), adminUsers)
	} else {
		log.Printf("Warning: No admin users configured. Admin functions disabled.")
	}

	bot, err := NewBot(token, storagePath, allowedUsers, adminUsers)
	if err != nil {
		log.Fatal("Failed to create bot:", err)
	}

	log.Printf("Bot started successfully. Storage path: %s", storagePath)

	// Ensure cleanup of resources on exit
	defer func() {
		if bot.statusService != nil {
			bot.statusService.Stop()
		}
	}()

	bot.Start()
}
