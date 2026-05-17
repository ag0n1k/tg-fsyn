package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SynologyClient defines the interface for interacting with DownloadStation.
type SynologyClient interface {
	FetchTasks() ([]Task, error)
	DeleteTasks(ids []string) error
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

// DeleteTasks removes the given task IDs from DownloadStation. The downloaded
// files on disk are not touched — only the task entries are removed.
func (c *synologyHTTPClient) DeleteTasks(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	sessionID, err := c.login()
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	endpoint := fmt.Sprintf(
		"http://%s:%s/webapi/DownloadStation/task.cgi?api=SYNO.DownloadStation.Task&method=delete&version=1&id=%s&force_complete=false&_sid=%s",
		c.host, c.port, url.QueryEscape(strings.Join(ids, ",")), sessionID,
	)

	resp, err := c.client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read delete response: %w", err)
	}

	var result struct {
		Success bool `json:"success"`
		Data    []struct {
			ID    string `json:"id"`
			Error int    `json:"error"`
		} `json:"data"`
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse delete response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("delete failed (code %d)", result.Error.Code)
	}

	var failed []string
	for _, r := range result.Data {
		if r.Error != 0 {
			failed = append(failed, fmt.Sprintf("%s (code %d)", r.ID, r.Error))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("some tasks were not deleted: %s", strings.Join(failed, ", "))
	}
	return nil
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
