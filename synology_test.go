package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDS emulates the slice of the DSM web API that AddTorrentSelective
// uses. Response shapes are copied from a real DownloadStation 4.1.
type fakeDS struct {
	t *testing.T

	mu           sync.Mutex
	uploaded     map[string]string // multipart fields
	torrent      string
	torrentName  string
	listFiles    []string // names as Task.List get reports them
	downloadArgs map[string]string
	statusCalls  int
	finishAfter  int
	listDeleted  bool
	stopped      bool
	createdTask  string
	failCreate   bool
}

func (f *fakeDS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(data any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}

	if r.URL.Path == "/webapi/auth.cgi" {
		reply(map[string]any{"sid": "test-sid"})
		return
	}
	if r.URL.Path != "/webapi/entry.cgi" {
		http.NotFound(w, r)
		return
	}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if r.URL.Query().Get("_sid") != "test-sid" {
			f.t.Errorf("upload without session")
		}
		mr, err := r.MultipartReader()
		if err != nil {
			f.t.Fatal(err)
		}
		f.uploaded = map[string]string{}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				f.t.Fatal(err)
			}
			b, _ := io.ReadAll(p)
			if p.FileName() != "" {
				f.torrent, f.torrentName = string(b), p.FileName()
				if ct := p.Header.Get("Content-Type"); ct != "application/x-bittorrent" {
					f.t.Errorf("torrent part content type %q", ct)
				}
				continue
			}
			f.uploaded[p.FormName()] = string(b)
		}
		if f.failCreate {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]any{"code": 403}})
			return
		}
		reply(map[string]any{"list_id": []string{"btdlLIST"}, "task_id": []string{}})
		return
	}

	if err := r.ParseForm(); err != nil {
		f.t.Fatal(err)
	}
	if r.PostForm.Get("_sid") != "test-sid" {
		f.t.Errorf("%s without session", r.PostForm.Get("api"))
	}
	call := r.PostForm.Get("api") + "." + r.PostForm.Get("method")
	switch call {
	case "SYNO.DownloadStation2.Task.List.get":
		if got := r.PostForm.Get("list_id"); got != `"btdlLIST"` {
			f.t.Errorf("list_id = %s, want JSON string", got)
		}
		files := []map[string]any{}
		for i, n := range f.listFiles {
			files = append(files, map[string]any{"index": i, "name": n, "size": 100})
		}
		reply(map[string]any{"files": files, "title": "Show.S01", "type": "bt"})
	case "SYNO.DownloadStation2.Task.List.delete":
		f.listDeleted = true
		reply(nil)
	case "SYNO.DownloadStation2.Task.List.Polling.download":
		f.downloadArgs = map[string]string{}
		for k := range r.PostForm {
			f.downloadArgs[k] = r.PostForm.Get(k)
		}
		reply(map[string]any{"task_id": "botuser/SYNODLTaskListDownload1"})
	case "SYNO.DownloadStation2.Task.List.Polling.download_status":
		if got := r.PostForm.Get("task_id"); got != `"botuser/SYNODLTaskListDownload1"` {
			f.t.Errorf("status task_id = %s", got)
		}
		f.statusCalls++
		if f.statusCalls < f.finishAfter {
			reply(map[string]any{"finish": false})
			return
		}
		reply(map[string]any{"finish": true, "success": true, "data": map[string]any{"task_id": []string{f.createdTask}}})
	case "SYNO.DownloadStation2.Task.List.Polling.download_stop":
		f.stopped = true
		reply(nil)
	default:
		f.t.Errorf("unexpected call %s", call)
		http.Error(w, "unexpected", http.StatusBadRequest)
	}
}

func newFakeDSClient(t *testing.T, ds *fakeDS) *synologyHTTPClient {
	t.Helper()
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	c := NewSynologyHTTPClient(host, port, "user", "pass")
	c.pollInterval = time.Millisecond
	c.pollTimeout = time.Second
	return c
}

func TestAddTorrentSelective(t *testing.T) {
	ds := &fakeDS{
		t:           t,
		listFiles:   []string{"Show.S01/E01.mkv", "Show.S01/E02.mkv", "Show.S01/E03.mkv", "Show.S01/E04.mkv"},
		finishAfter: 3,
		createdTask: "dbid_231",
	}
	c := newFakeDSClient(t, ds)

	id, err := c.AddTorrentSelective([]byte("d4:infodee"), `we"ird.torrent`, "video/.tg-fsyn-staging",
		map[string]bool{"Show.S01/E02.mkv": true, "Show.S01/E04.mkv": true})
	if err != nil {
		t.Fatal(err)
	}
	if id != "dbid_231" {
		t.Errorf("task id = %q", id)
	}

	for k, want := range map[string]string{
		"api":         "SYNO.DownloadStation2.Task",
		"method":      "create",
		"version":     "2",
		"type":        `"file"`,
		"file":        `["torrent"]`,
		"destination": `"video/.tg-fsyn-staging"`,
		"create_list": "true",
	} {
		if got := ds.uploaded[k]; got != want {
			t.Errorf("upload field %s = %q, want %q", k, got, want)
		}
	}
	if ds.torrent != "d4:infodee" || ds.torrentName != `we"ird.torrent` {
		t.Errorf("uploaded torrent %q named %q", ds.torrent, ds.torrentName)
	}
	for k, want := range map[string]string{
		"list_id":          `"btdlLIST"`,
		"destination":      `"video/.tg-fsyn-staging"`,
		"create_subfolder": "true",
		"selected":         "[1,3]",
		"version":          "2",
	} {
		if got := ds.downloadArgs[k]; got != want {
			t.Errorf("download param %s = %q, want %q", k, got, want)
		}
	}
	if ds.statusCalls != 3 || !ds.stopped || ds.listDeleted {
		t.Errorf("statusCalls=%d stopped=%v listDeleted=%v", ds.statusCalls, ds.stopped, ds.listDeleted)
	}
}

func TestAddTorrentSelectiveDropsListOnMismatch(t *testing.T) {
	ds := &fakeDS{t: t, listFiles: []string{"Show.S01/E01.mkv"}}
	c := newFakeDSClient(t, ds)

	_, err := c.AddTorrentSelective([]byte("x"), "s.torrent", "video/st", map[string]bool{"Show.S01/E09.mkv": true})
	if err == nil || !strings.Contains(err.Error(), "0 of the 1") {
		t.Fatalf("err = %v", err)
	}
	if !ds.listDeleted {
		t.Error("pending list must be deleted")
	}
	if ds.downloadArgs != nil {
		t.Error("nothing must be submitted")
	}
}

func TestAddTorrentSelectiveUploadError(t *testing.T) {
	ds := &fakeDS{t: t, failCreate: true}
	c := newFakeDSClient(t, ds)
	_, err := c.AddTorrentSelective([]byte("x"), "s.torrent", "video/st", map[string]bool{"a/b": true})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v", err)
	}
}

func TestAddTorrentSelectiveTimesOut(t *testing.T) {
	ds := &fakeDS{t: t, listFiles: []string{"S/a"}, finishAfter: 1 << 30}
	c := newFakeDSClient(t, ds)
	c.pollTimeout = 20 * time.Millisecond
	_, err := c.AddTorrentSelective([]byte("x"), "s.torrent", "video/st", map[string]bool{"S/a": true})
	if err == nil || !strings.Contains(err.Error(), "did not create") {
		t.Errorf("err = %v", err)
	}
	if !ds.stopped {
		t.Error("polling task must be stopped")
	}
}

func TestTaskDecodesV1FileFields(t *testing.T) {
	raw := `{"id":"dbid_230","title":"S","status":"seeding","additional":{"detail":{"destination":"video/.tg-fsyn-staging"},
		"file":[{"filename":"E01.mkv","index":0,"priority":"normal","size":10,"size_downloaded":10,"wanted":true},
		        {"filename":"E02.mkv","index":1,"priority":"normal","size":10,"size_downloaded":3,"wanted":false}]}}`
	var task Task
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatal(err)
	}
	f := task.Additional.File
	if task.Additional.Detail.Destination != "video/.tg-fsyn-staging" || len(f) != 2 {
		t.Fatalf("task = %+v", task)
	}
	if got := fmt.Sprintf("%s %d %v %v", f[0].Filename, f[0].SizeDownloaded, f[0].IsWanted(), f[1].IsWanted()); got != "E01.mkv 10 true false" {
		t.Errorf("files decoded as %s", got)
	}
}
