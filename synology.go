package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SynologyClient defines the interface for fetching download tasks.
type SynologyClient interface {
	FetchTasks() ([]Task, error)
}

// synologyHTTPClient implements SynologyClient using the Synology DownloadStation HTTP API.
type synologyHTTPClient struct {
	client   *http.Client
	host     string
	port     string
	username string
	password string
}

func NewSynologyHTTPClient(host, port, username, password string) *synologyHTTPClient {
	return &synologyHTTPClient{
		client:   &http.Client{Timeout: 30 * time.Second},
		host:     host,
		port:     port,
		username: username,
		password: password,
	}
}

func (c *synologyHTTPClient) FetchTasks() ([]Task, error) {
	sessionID, err := c.login()
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	tasks, err := c.getDownloadTasks(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}

	return tasks, nil
}

func (c *synologyHTTPClient) login() (string, error) {
	url := fmt.Sprintf("http://%s:%s/webapi/auth.cgi?api=SYNO.API.Auth&method=login&version=7&account=%s&passwd=%s&format=json", c.host, c.port, c.username, c.password)

	resp, err := c.client.Get(url)
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

func (c *synologyHTTPClient) getDownloadTasks(sessionID string) ([]Task, error) {
	url := fmt.Sprintf("http://%s:%s/webapi/DownloadStation/task.cgi?api=SYNO.DownloadStation.Task&method=list&version=1&_sid=%s&additional=detail,file", c.host, c.port, sessionID)

	resp, err := c.client.Get(url)
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
