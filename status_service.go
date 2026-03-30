package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// BotSender abstracts the Telegram bot API Send method for testability.
type BotSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

// StatusService manages the status monitoring with periodic polling.
type StatusService struct {
	mu               sync.RWMutex
	tasks            []Task
	lastChecked      time.Time
	previousStatuses map[string]string

	synology     SynologyClient
	adminUsers   map[int64]bool
	botAPI       BotSender
	tickInterval time.Duration
	stopCh       chan struct{}
}

// NewStatusService creates a new status service.
func NewStatusService(synology SynologyClient, adminUsers map[int64]bool, botAPI BotSender, tickInterval time.Duration) *StatusService {
	return &StatusService{
		synology:         synology,
		adminUsers:       adminUsers,
		botAPI:           botAPI,
		tickInterval:     tickInterval,
		previousStatuses: make(map[string]string),
		stopCh:           make(chan struct{}),
	}
}

// Start begins the status monitoring loop.
// The ticker always runs at the configured interval and never stops.
func (s *StatusService) Start() {
	go func() {
		s.checkStatus()

		ticker := time.NewTicker(s.tickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.checkStatus()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop stops the status monitoring gracefully.
func (s *StatusService) Stop() {
	close(s.stopCh)
}

// checkStatus fetches and processes the current status.
func (s *StatusService) checkStatus() {
	tasks, err := s.synology.FetchTasks()
	if err != nil {
		log.Printf("Failed to fetch tasks: %v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks = tasks
	s.lastChecked = time.Now()

	for _, task := range tasks {
		if prevStatus, exists := s.previousStatuses[task.ID]; exists {
			if prevStatus != task.Status {
				s.notifyStatusChange(task, prevStatus)
			}
		}
		s.previousStatuses[task.ID] = task.Status
	}
}

// GetStatus returns the current cached status information.
func (s *StatusService) GetStatus() ([]Task, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks, s.lastChecked
}

// HasRunningTasks checks if there are any non-finished tasks.
func (s *StatusService) HasRunningTasks() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, task := range s.tasks {
		if task.Status != "finished" {
			return true
		}
	}
	return false
}

// FormatStatusMessage formats status information for display.
func (s *StatusService) FormatStatusMessage() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.tasks) == 0 {
		return "No download tasks found."
	}

	result := fmt.Sprintf("📋 Current Download Status (as of %s)\n\n", s.lastChecked.Format("2006-01-02 15:04:05"))

	for _, task := range s.tasks {
		result += fmt.Sprintf("📦 %s\n", task.Title)
		result += fmt.Sprintf("   Status: %s\n", task.Status)
		result += fmt.Sprintf("   Size: %.2f GB\n", float64(task.Size)/(1024*1024*1024))

		if task.Additional.Detail.CompletedTime > task.Additional.Detail.StartedTime {
			duration := task.Additional.Detail.CompletedTime - task.Additional.Detail.StartedTime
			hours := float64(duration) / (60 * 60)
			result += fmt.Sprintf("   ⬇️ Downloaded: %.2f hours\n", hours)

			if duration > 0 {
				speed := float64(task.Size) / float64(duration)
				result += fmt.Sprintf("   ⬇️ Average Speed: %.2f MB/s\n", speed/(1024*1024))
			}
		}
		result += "\n"
	}

	return result
}

// notifyStatusChange sends a notification to admin users when a task status changes.
// Must be called with s.mu held.
func (s *StatusService) notifyStatusChange(task Task, previousStatus string) {
	log.Printf("Task status changed: %s (was %s, now %s)", task.Title, previousStatus, task.Status)

	if s.botAPI != nil {
		message := fmt.Sprintf("🔔 Status Change Alert:\n\nTask: %s\nPrevious Status: %s\nNew Status: %s\n\nLast updated: %s",
			task.Title, previousStatus, task.Status, time.Now().Format("2006-01-02 15:04:05"))

		for userID := range s.adminUsers {
			msg := tgbotapi.NewMessage(userID, message)
			_, err := s.botAPI.Send(msg)
			if err != nil {
				log.Printf("Failed to send status change notification to user %d: %v", userID, err)
			}
		}
	}
}
