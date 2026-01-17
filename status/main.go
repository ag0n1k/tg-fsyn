package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
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

func main() {
	// Get configuration from environment variables
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

	// Create HTTP client
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Login to DSM
	sessionID, err := login(client, host, port, username, password)
	if err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	// Get download tasks
	tasks, err := getDownloadTasks(client, host, port, sessionID)
	if err != nil {
		log.Fatalf("Failed to get tasks: %v", err)
	}

	fmt.Printf("Found %d tasks\n", len(tasks))
	for _, task := range tasks {
		fmt.Printf("📦 %s\n", task.Title)
		fmt.Printf("   Status: %s\n", task.Status)
		fmt.Printf("   Size: %.2f GB\n", float64(task.Size)/(1024*1024*1024))
		
		if task.Additional.Detail.CompletedTime > task.Additional.Detail.StartedTime {
			duration := task.Additional.Detail.CompletedTime - task.Additional.Detail.StartedTime
			hours := float64(duration) / (60 * 60)
			fmt.Printf("   ⬇️ Downloaded: %.2f hours\n", hours)
			
			if duration > 0 {
				speed := float64(task.Size) / float64(duration)
				fmt.Printf("   ⬇️ Average Speed: %.2f MB/s\n", speed/(1024*1024))
			}
		}
	}
}

func login(client *http.Client, host, port, username, password string) (string, error) {
	url := fmt.Sprintf("http://%s:%s/webapi/auth.cgi?api=SYNO.API.Auth&method=login&version=7&account=%s&passwd=%s&format=json", host, port, username, password)
	
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read login response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse login response: %w", err)
	}

	if result["success"] != true {
		return "", fmt.Errorf("login failed: %s", string(body))
	}

	data := result["data"].(map[string]interface{})
	sessionID := data["sid"].(string)
	return sessionID, nil
}

func getDownloadTasks(client *http.Client, host, port, sessionID string) ([]Task, error) {
	url := fmt.Sprintf("http://%s:%s/webapi/DownloadStation/task.cgi?api=SYNO.DownloadStation.Task&method=list&version=1&_sid=%s&additional=detail,file", host, port, sessionID)
	
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("task list request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read task list response: %w", err)
	}

	var rawResponse map[string]interface{}
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return nil, fmt.Errorf("failed to parse raw task list response: %w", err)
	}
	
	// Extract tasks from response
	var tasks []Task
	
	if data, ok := rawResponse["data"]; ok {
		if dataMap, ok := data.(map[string]interface{}); ok {
			if tasksArray, ok := dataMap["tasks"]; ok {
				if tasksSlice, ok := tasksArray.([]interface{}); ok {
					for _, taskData := range tasksSlice {
						if taskMap, ok := taskData.(map[string]interface{}); ok {
							taskJSON, _ := json.Marshal(taskMap)
							var task Task
							if err := json.Unmarshal(taskJSON, &task); err == nil {
								tasks = append(tasks, task)
							}
						}
					}
				}
			}
		}
	}
	
	return tasks, nil
}