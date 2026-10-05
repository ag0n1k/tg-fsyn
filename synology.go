package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// synologyHTTPTimeout is generous on purpose: while DownloadStation
	// hash-checks a big torrent the NAS disks are saturated and even a task
	// list takes 10-30 s; a 30 s timeout made every poll fail for the whole
	// check.
	synologyHTTPTimeout = 2 * time.Minute

	// taskListPollInterval/taskListPollTimeout drive the asynchronous
	// "create task from list" call (Task.List.Polling).
	taskListPollInterval = time.Second
	taskListPollTimeout  = 2 * time.Minute
)

// SynologyClient defines the interface for interacting with DownloadStation.
type SynologyClient interface {
	FetchTasks() ([]Task, error)
	DeleteTasks(ids []string) error
	// AddTorrentSelective queues a .torrent with only the wanted files
	// selected and returns the new task ID. destination is share-relative
	// ("video/.tg-fsyn-staging"); wanted holds paths the way DownloadStation
	// lists them: "<torrent name>/<path inside the torrent>".
	AddTorrentSelective(torrent []byte, fileName, destination string, wanted map[string]bool) (string, error)
}

// synologyHTTPClient implements SynologyClient using the Synology DownloadStation HTTP API.
type synologyHTTPClient struct {
	client       *http.Client
	host         string
	port         string
	username     string
	password     string
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func NewSynologyHTTPClient(host, port, username, password string) *synologyHTTPClient {
	return &synologyHTTPClient{
		client:       &http.Client{Timeout: synologyHTTPTimeout},
		host:         host,
		port:         port,
		username:     username,
		password:     password,
		pollInterval: taskListPollInterval,
		pollTimeout:  taskListPollTimeout,
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

// synoResponse is the envelope every DSM web API answers with.
type synoResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   struct {
		Code int `json:"code"`
	} `json:"error"`
}

func decodeSynoResponse(body []byte, out any) error {
	var r synoResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	if !r.Success {
		return fmt.Errorf("API error code %d", r.Error.Code)
	}
	if out != nil && len(r.Data) > 0 {
		if err := json.Unmarshal(r.Data, out); err != nil {
			return fmt.Errorf("failed to parse response data: %w", err)
		}
	}
	return nil
}

// callEntry POSTs a DownloadStation2 request to entry.cgi. Every parameter
// value is JSON-encoded (strings quoted, lists as arrays), which is how DSM's
// own web UI talks to these APIs.
func (c *synologyHTTPClient) callEntry(sid, api, method string, version int, params map[string]any, out any) error {
	form := url.Values{
		"api":     {api},
		"method":  {method},
		"version": {strconv.Itoa(version)},
		"_sid":    {sid},
	}
	for k, v := range params {
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("%s.%s: encode %s: %w", api, method, k, err)
		}
		form.Set(k, string(b))
	}
	resp, err := c.client.PostForm(fmt.Sprintf("http://%s:%s/webapi/entry.cgi", c.host, c.port), form)
	if err != nil {
		return fmt.Errorf("%s.%s request failed: %w", api, method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s.%s: failed to read response: %w", api, method, err)
	}
	if err := decodeSynoResponse(body, out); err != nil {
		return fmt.Errorf("%s.%s: %w", api, method, err)
	}
	return nil
}

// AddTorrentSelective mirrors what the DownloadStation UI does when "show
// file info" is ticked: upload the torrent as a pending list, pick files by
// index, then submit the list as a task.
func (c *synologyHTTPClient) AddTorrentSelective(torrent []byte, fileName, destination string, wanted map[string]bool) (string, error) {
	if len(wanted) == 0 {
		return "", errors.New("no files selected")
	}
	sid, err := c.login()
	if err != nil {
		return "", fmt.Errorf("login failed: %w", err)
	}

	listID, err := c.createTaskList(sid, torrent, fileName, destination)
	if err != nil {
		return "", err
	}

	var list struct {
		Files []struct {
			Index int    `json:"index"`
			Name  string `json:"name"`
		} `json:"files"`
	}
	if err := c.callEntry(sid, "SYNO.DownloadStation2.Task.List", "get", 2, map[string]any{"list_id": listID}, &list); err != nil {
		c.deleteTaskList(sid, listID)
		return "", err
	}
	selected := []int{}
	for _, f := range list.Files {
		if wanted[f.Name] {
			selected = append(selected, f.Index)
		}
	}
	if len(selected) != len(wanted) {
		c.deleteTaskList(sid, listID)
		return "", fmt.Errorf("DownloadStation lists %d of the %d wanted files", len(selected), len(wanted))
	}

	var started struct {
		TaskID string `json:"task_id"`
	}
	err = c.callEntry(sid, "SYNO.DownloadStation2.Task.List.Polling", "download", 2, map[string]any{
		"list_id":          listID,
		"destination":      destination,
		"create_subfolder": true,
		"selected":         selected,
	}, &started)
	if err != nil {
		c.deleteTaskList(sid, listID)
		return "", err
	}
	defer func() {
		if err := c.callEntry(sid, "SYNO.DownloadStation2.Task.List.Polling", "download_stop", 2, map[string]any{"task_id": started.TaskID}, nil); err != nil {
			log.Printf("Task list polling cleanup failed: %v", err)
		}
	}()

	deadline := time.Now().Add(c.pollTimeout)
	for {
		var status struct {
			Finish  bool `json:"finish"`
			Success bool `json:"success"`
			Data    struct {
				TaskID []string `json:"task_id"`
			} `json:"data"`
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := c.callEntry(sid, "SYNO.DownloadStation2.Task.List.Polling", "download_status", 2, map[string]any{"task_id": started.TaskID}, &status); err != nil {
			return "", err
		}
		if status.Finish {
			if !status.Success {
				return "", fmt.Errorf("DownloadStation rejected the task (code %d)", status.Error.Code)
			}
			if len(status.Data.TaskID) == 0 {
				return "", errors.New("DownloadStation created no task")
			}
			return status.Data.TaskID[0], nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("DownloadStation did not create the task within %v", c.pollTimeout)
		}
		time.Sleep(c.pollInterval)
	}
}

// createTaskList uploads the torrent with create_list=true, so nothing is
// downloaded yet, and returns the list ID.
func (c *synologyHTTPClient) createTaskList(sid string, torrent []byte, fileName, destination string) (string, error) {
	dest, err := json.Marshal(destination)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range [][2]string{
		{"api", "SYNO.DownloadStation2.Task"},
		{"method", "create"},
		{"version", "2"},
		{"type", `"file"`},
		{"file", `["torrent"]`},
		{"destination", string(dest)},
		{"create_list", "true"},
	} {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return "", err
		}
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="torrent"; filename="%s"`, quoteEscaper.Replace(fileName)))
	h.Set("Content-Type", "application/x-bittorrent")
	part, err := w.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(torrent); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("http://%s:%s/webapi/entry.cgi?_sid=%s", c.host, c.port, url.QueryEscape(sid))
	resp, err := c.client.Post(endpoint, w.FormDataContentType(), &buf)
	if err != nil {
		return "", fmt.Errorf("torrent upload failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response: %w", err)
	}
	var created struct {
		ListID []string `json:"list_id"`
	}
	if err := decodeSynoResponse(body, &created); err != nil {
		return "", fmt.Errorf("torrent upload: %w", err)
	}
	if len(created.ListID) == 0 {
		return "", errors.New("torrent upload returned no list")
	}
	return created.ListID[0], nil
}

func (c *synologyHTTPClient) deleteTaskList(sid, listID string) {
	if err := c.callEntry(sid, "SYNO.DownloadStation2.Task.List", "delete", 2, map[string]any{"list_id": listID}, nil); err != nil {
		log.Printf("Failed to drop pending task list: %v", err)
	}
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)
