//go:build integration

package main

// Runs against a real DownloadStation. Build for the NAS and run it from the
// bot's directory so it picks up the bot's .env:
//
//	GOOS=linux GOARCH=arm64 go test -c -tags integration -o tg-fsyn-it .
//	scp -O tg-fsyn-it <nas>:tg-fsyn/
//	ssh <nas> 'cd ~/tg-fsyn && TGFSYN_IT_TORRENT=/path/x.torrent TGFSYN_IT_DEST=video/.tg-fsyn-staging ./tg-fsyn-it -test.v'
//
// The torrent must be multi-file; its last file is selected. The created task
// is deleted at the end, so nothing is downloaded beyond a few seconds.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
)

func TestIntegrationAddTorrentSelective(t *testing.T) {
	_ = godotenv.Load()
	torrentPath, dest := os.Getenv("TGFSYN_IT_TORRENT"), os.Getenv("TGFSYN_IT_DEST")
	if torrentPath == "" || dest == "" {
		t.Skip("set TGFSYN_IT_TORRENT and TGFSYN_IT_DEST")
	}
	host := os.Getenv("SYNOLOGY_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("SYNOLOGY_PORT")
	if port == "" {
		port = "5000"
	}
	c := NewSynologyHTTPClient(host, port, os.Getenv("SYNOLOGY_USERNAME"), os.Getenv("SYNOLOGY_PASSWORD"))

	data, err := os.ReadFile(torrentPath)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := ParseTorrent(data)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.MultiFile() {
		t.Fatal("need a multi-file torrent")
	}
	last := meta.Files[len(meta.Files)-1]

	id, err := c.AddTorrentSelective(data, filepath.Base(torrentPath), dest, map[string]bool{meta.Name + "/" + last.Path: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created %s", id)
	defer func() {
		if err := c.DeleteTasks([]string{id}); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()

	tasks, err := c.FetchTasks()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.ID != id {
			continue
		}
		t.Logf("status=%s title=%s destination=%s", task.Status, task.Title, task.Additional.Detail.Destination)
		if task.Additional.Detail.Destination != dest {
			t.Errorf("destination = %q, want %q", task.Additional.Detail.Destination, dest)
		}
		if len(task.Additional.File) == 0 {
			// DownloadStation lists files only once the task has started.
			t.Logf("file selection not verifiable: DownloadStation reports no files for a %s task", task.Status)
		}
		for _, f := range task.Additional.File {
			t.Logf("  %s wanted=%v", f.Filename, f.IsWanted())
			if f.IsWanted() != (f.Filename == last.Path) {
				t.Errorf("%s: wanted=%v", f.Filename, f.IsWanted())
			}
		}
		return
	}
	t.Fatalf("task %s not in the task list", id)
}
