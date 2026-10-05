package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testStagingDest = "video/.tg-fsyn-staging"

type testUpdater struct {
	*SeasonUpdater
	client  *mockSynologyClient
	notes   []string
	indexed []string
}

// newTestUpdater lays out <tmp>/video (download dir, staging inside it) and
// <tmp>/torrents (watch folder). NewSeasonUpdater insists on /volumeN paths,
// so the struct is filled in directly.
func newTestUpdater(t *testing.T) *testUpdater {
	t.Helper()
	root := t.TempDir()
	client := &mockSynologyClient{addTaskID: "dbid_9"}
	tu := &testUpdater{client: client}
	tu.SeasonUpdater = &SeasonUpdater{
		synology:    client,
		downloadDir: filepath.Join(root, "video"),
		stagingDir:  filepath.Join(root, "video", DefaultStagingDirName),
		stagingDest: testStagingDest,
		watchDir:    filepath.Join(root, "torrents"),
		pending:     map[string]*pendingTorrent{},
		reported:    map[string]bool{},
		now:         time.Now,
	}
	tu.notify = func(s string) { tu.notes = append(tu.notes, s) }
	tu.indexFile = func(p string) error { tu.indexed = append(tu.indexed, p); return nil }
	for _, d := range []string{tu.downloadDir, tu.watchDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return tu
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func episodes(n int) []TorrentFile {
	var out []TorrentFile
	for i := 1; i <= n; i++ {
		out = append(out, TorrentFile{fmt.Sprintf("Show.S01E%02d.mkv", i), int64(1000 + i)})
	}
	return out
}

func wantedPtr(b bool) *bool { return &b }

func stagingTask(id, title string, files ...TaskFile) Task {
	t := Task{ID: id, Title: title, Status: "seeding"}
	t.Additional.Detail.Destination = testStagingDest
	t.Additional.File = files
	return t
}

func TestDSDestination(t *testing.T) {
	for in, want := range map[string]string{
		"/volume1/video/.tg-fsyn-staging": "video/.tg-fsyn-staging",
		"/volume1/video/":                 "video",
		"/volumeUSB2/usbshare/x":          "usbshare/x",
	} {
		got, err := dsDestination(in)
		if err != nil || got != want {
			t.Errorf("dsDestination(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"/volume1", "/home/x/y", "video/x", "/"} {
		if got, err := dsDestination(in); err == nil {
			t.Errorf("dsDestination(%q) = %q, want an error", in, got)
		}
	}
}

func TestNewSeasonUpdaterRequiresSameShare(t *testing.T) {
	if _, err := NewSeasonUpdater(&mockSynologyClient{}, "/volume1/video", "/volume1/torrents/.staging", "/tmp"); err == nil {
		t.Error("staging on another share must be rejected")
	}
	u, err := NewSeasonUpdater(&mockSynologyClient{}, "/volume1/video", "/volume1/video/.staging", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if u.stagingDest != "video/.staging" {
		t.Errorf("stagingDest = %q", u.stagingDest)
	}
}

func TestPlanSeasonUpdate(t *testing.T) {
	dir := t.TempDir()
	meta := &TorrentMeta{Name: "Show.S01", Files: episodes(3)}

	if plan, err := PlanSeasonUpdate(meta, dir); plan != nil || err != nil {
		t.Fatalf("no season folder: got %+v, %v", plan, err)
	}

	season := filepath.Join(dir, "Show.S01")
	writeTestFile(t, filepath.Join(season, "unrelated.txt"), "x")
	if plan, err := PlanSeasonUpdate(meta, dir); plan != nil || err != nil {
		t.Fatalf("folder without any torrent file: got %+v, %v", plan, err)
	}

	writeTestFile(t, filepath.Join(season, "Show.S01E01.mkv"), "1")
	writeTestFile(t, filepath.Join(season, "Show.S01E02.mkv"), "a different size")
	writeTestFile(t, filepath.Join(season, "Show.S01E03.mkv.part"), "half")
	plan, err := PlanSeasonUpdate(meta, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Present) != 2 || len(plan.Missing) != 1 || plan.Missing[0].Path != "Show.S01E03.mkv" {
		t.Errorf("plan = %+v", plan)
	}

	single := &TorrentMeta{Name: "Show.S01"}
	if plan, err := PlanSeasonUpdate(single, dir); plan != nil || err != nil {
		t.Errorf("single-file torrent: got %+v, %v", plan, err)
	}
}

func TestHandleTorrentNotAnUpdate(t *testing.T) {
	tu := newTestUpdater(t)
	out := tu.HandleTorrent(multiFileTorrent("Show.S01", episodes(2)...), "show.torrent")
	if out.Handled {
		t.Errorf("fresh season must go to the watch folder, got %+v", out)
	}
	if out := tu.HandleTorrent([]byte("not a torrent"), "x.torrent"); out.Handled {
		t.Errorf("unparsable torrent must go to the watch folder, got %+v", out)
	}
	if len(tu.client.added) != 0 {
		t.Errorf("no task should be added, got %d", len(tu.client.added))
	}
}

func TestHandleTorrentQueuesOnlyMissingFiles(t *testing.T) {
	tu := newTestUpdater(t)
	season := filepath.Join(tu.downloadDir, "Show.S01")
	writeTestFile(t, filepath.Join(season, "Show.S01E01.mkv"), "1")
	writeTestFile(t, filepath.Join(season, "Show.S01E02.mkv"), "2")
	data := multiFileTorrent("Show.S01", episodes(3)...)

	out := tu.HandleTorrent(data, "rutracker-1.torrent")

	if !out.Handled || out.PendingID == "" {
		t.Fatalf("outcome = %+v", out)
	}
	if !strings.Contains(out.Text, "2 of 3") || !strings.Contains(out.Text, "Show.S01E03.mkv") {
		t.Errorf("message does not describe the update:\n%s", out.Text)
	}
	if len(tu.client.added) != 1 {
		t.Fatalf("expected one selective add, got %d", len(tu.client.added))
	}
	call := tu.client.added[0]
	if call.destination != testStagingDest || call.fileName != "rutracker-1.torrent" || !bytes.Equal(call.torrent, data) {
		t.Errorf("add call = dest %q file %q", call.destination, call.fileName)
	}
	if len(call.wanted) != 1 || !call.wanted["Show.S01/Show.S01E03.mkv"] {
		t.Errorf("wanted = %v", call.wanted)
	}
	if !exists(tu.stagingDir) {
		t.Error("staging dir was not created")
	}
}

func TestHandleTorrentNothingNew(t *testing.T) {
	tu := newTestUpdater(t)
	for _, f := range episodes(2) {
		writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", f.Path), "x")
	}
	out := tu.HandleTorrent(multiFileTorrent("Show.S01", episodes(2)...), "s.torrent")
	if !out.Handled || out.PendingID == "" || !strings.Contains(out.Text, "already on disk") {
		t.Errorf("outcome = %+v", out)
	}
	if len(tu.client.added) != 0 {
		t.Error("nothing should be queued")
	}
}

func TestHandleTorrentAddFailureQueuesNothing(t *testing.T) {
	tu := newTestUpdater(t)
	writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E01.mkv"), "1")
	tu.client.addErr = errors.New("API error code 403")

	out := tu.HandleTorrent(multiFileTorrent("Show.S01", episodes(2)...), "s.torrent")

	if !out.Handled || out.PendingID == "" || !strings.Contains(out.Text, "Nothing was queued") {
		t.Errorf("outcome = %+v", out)
	}
	if entries, _ := os.ReadDir(tu.watchDir); len(entries) != 0 {
		t.Error("the torrent must not silently fall back to the watch folder")
	}
}

func TestDownloadFullCancelsSelectiveTask(t *testing.T) {
	tu := newTestUpdater(t)
	writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E01.mkv"), "1")
	data := multiFileTorrent("Show.S01", episodes(2)...)
	out := tu.HandleTorrent(data, "s.torrent")
	writeTestFile(t, filepath.Join(tu.stagingDir, "Show.S01", "Show.S01E02.mkv.part"), "partial")

	name, err := tu.DownloadFull(out.PendingID)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Show.S01" {
		t.Errorf("name = %q", name)
	}
	if fmt.Sprint(tu.client.deletedIDs) != "[dbid_9]" {
		t.Errorf("deleted = %v", tu.client.deletedIDs)
	}
	if exists(filepath.Join(tu.stagingDir, "Show.S01")) {
		t.Error("staging folder of the cancelled task was left behind")
	}
	if got := readTestFile(t, filepath.Join(tu.watchDir, "s.torrent")); got != string(data) {
		t.Error("watch folder got different torrent bytes")
	}
	if _, err := tu.DownloadFull(out.PendingID); !errors.Is(err, errPendingExpired) {
		t.Errorf("second press: err = %v", err)
	}
}

func TestDownloadFullKeepsTorrentWhenCancelFails(t *testing.T) {
	tu := newTestUpdater(t)
	writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E01.mkv"), "1")
	out := tu.HandleTorrent(multiFileTorrent("Show.S01", episodes(2)...), "s.torrent")
	tu.client.deleteErr = errors.New("boom")

	if _, err := tu.DownloadFull(out.PendingID); err == nil {
		t.Fatal("expected an error")
	}
	if exists(filepath.Join(tu.watchDir, "s.torrent")) {
		t.Error("full torrent queued while the selective task still runs")
	}

	tu.client.deleteErr = nil
	if _, err := tu.DownloadFull(out.PendingID); err != nil {
		t.Errorf("retry failed: %v", err)
	}
}

func TestDownloadFullUnknownID(t *testing.T) {
	tu := newTestUpdater(t)
	if _, err := tu.DownloadFull("nope"); !errors.Is(err, errPendingExpired) {
		t.Errorf("err = %v", err)
	}
}

func TestPendingTorrentsAreBounded(t *testing.T) {
	tu := newTestUpdater(t)
	var ids []string
	for i := 0; i < maxPendingTorrents+5; i++ {
		ids = append(ids, tu.remember(&pendingTorrent{name: fmt.Sprint(i)}))
	}
	if len(tu.pending) != maxPendingTorrents || len(tu.order) != maxPendingTorrents {
		t.Errorf("pending = %d, order = %d", len(tu.pending), len(tu.order))
	}
	if tu.pending[ids[0]] != nil || tu.pending[ids[len(ids)-1]] == nil {
		t.Error("the oldest entries must be evicted first")
	}
}

// seasonWithStagedEpisode prepares E01/E02 in the season folder and a
// finished E03 plus DownloadStation's boundary-piece stub of E02 in staging.
func seasonWithStagedEpisode(t *testing.T, tu *testUpdater) Task {
	t.Helper()
	writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E01.mkv"), "old 1")
	writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E02.mkv"), "old 2")
	writeTestFile(t, filepath.Join(tu.stagingDir, "Show.S01", "Show.S01E02.mkv.part"), "stub")
	writeTestFile(t, filepath.Join(tu.stagingDir, "Show.S01", "Show.S01E03.mkv"), "new 3")
	return stagingTask("dbid_9", "Show.S01",
		TaskFile{Filename: "Show.S01E01.mkv", Size: 5, SizeDownloaded: 0, Wanted: wantedPtr(false)},
		TaskFile{Filename: "Show.S01E02.mkv", Size: 5, SizeDownloaded: 4, Wanted: wantedPtr(false)},
		TaskFile{Filename: "Show.S01E03.mkv", Size: 5, SizeDownloaded: 5, Wanted: wantedPtr(true)},
	)
}

func TestProcessTasksMovesCompletedFiles(t *testing.T) {
	tu := newTestUpdater(t)
	task := seasonWithStagedEpisode(t, tu)

	tu.ProcessTasks([]Task{task})

	moved := filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E03.mkv")
	if got := readTestFile(t, moved); got != "new 3" {
		t.Errorf("moved file content = %q", got)
	}
	if got := readTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E02.mkv")); got != "old 2" {
		t.Errorf("existing episode was touched: %q", got)
	}
	if exists(filepath.Join(tu.stagingDir, "Show.S01")) {
		t.Error("staging folder (with the stub) was not removed")
	}
	if fmt.Sprint(tu.client.deletedIDs) != "[dbid_9]" {
		t.Errorf("deleted = %v", tu.client.deletedIDs)
	}
	if fmt.Sprint(tu.indexed) != fmt.Sprint([]string{moved}) {
		t.Errorf("indexed = %v", tu.indexed)
	}
	if len(tu.notes) != 1 || !strings.Contains(tu.notes[0], "moved 1 new file") {
		t.Errorf("notes = %v", tu.notes)
	}
}

func TestProcessTasksWaitsForCompletion(t *testing.T) {
	tu := newTestUpdater(t)
	task := seasonWithStagedEpisode(t, tu)
	task.Additional.File[2].SizeDownloaded = 4

	tu.ProcessTasks([]Task{task})

	if exists(filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E03.mkv")) || len(tu.client.deletedIDs) != 0 || len(tu.notes) != 0 {
		t.Error("an incomplete task must be left alone")
	}
}

func TestProcessTasksWaitsForPartRename(t *testing.T) {
	tu := newTestUpdater(t)
	task := seasonWithStagedEpisode(t, tu)
	staged := filepath.Join(tu.stagingDir, "Show.S01", "Show.S01E03.mkv")
	if err := os.Rename(staged, staged+".part"); err != nil {
		t.Fatal(err)
	}

	tu.ProcessTasks([]Task{task})

	if len(tu.client.deletedIDs) != 0 || len(tu.notes) != 0 || !exists(staged+".part") {
		t.Error("must wait until DownloadStation renames the .part file")
	}
}

func TestProcessTasksNeverOverwrites(t *testing.T) {
	tu := newTestUpdater(t)
	task := seasonWithStagedEpisode(t, tu)
	writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E03.mkv"), "user's copy")

	tu.ProcessTasks([]Task{task})
	tu.ProcessTasks([]Task{task})

	if got := readTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E03.mkv")); got != "user's copy" {
		t.Errorf("existing file overwritten: %q", got)
	}
	if !exists(filepath.Join(tu.stagingDir, "Show.S01", "Show.S01E03.mkv")) {
		t.Error("staged file must be kept for a manual look")
	}
	if len(tu.client.deletedIDs) != 0 {
		t.Error("task must be kept")
	}
	if len(tu.notes) != 1 || !strings.Contains(tu.notes[0], "already exists") {
		t.Errorf("expected exactly one report, got %v", tu.notes)
	}
}

func TestProcessTasksRetriesTaskDeletion(t *testing.T) {
	tu := newTestUpdater(t)
	task := seasonWithStagedEpisode(t, tu)
	tu.client.deleteErr = errors.New("DS busy")

	tu.ProcessTasks([]Task{task})
	if !exists(filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E03.mkv")) {
		t.Fatal("file should already be moved")
	}
	if len(tu.notes) != 0 {
		t.Errorf("nothing to report yet, got %v", tu.notes)
	}

	tu.client.deleteErr = nil
	tu.ProcessTasks([]Task{task})
	if fmt.Sprint(tu.client.deletedIDs) != "[dbid_9]" {
		t.Errorf("deleted = %v", tu.client.deletedIDs)
	}
	if exists(filepath.Join(tu.stagingDir, "Show.S01")) {
		t.Error("staging folder not removed on retry")
	}
	if len(tu.notes) != 1 || !strings.Contains(tu.notes[0], "Show.S01E03.mkv") {
		t.Errorf("notes = %v", tu.notes)
	}
}

func TestProcessTasksIgnoresOtherTasks(t *testing.T) {
	tu := newTestUpdater(t)
	task := seasonWithStagedEpisode(t, tu)
	task.Additional.Detail.Destination = "video"

	tu.ProcessTasks([]Task{task})

	if len(tu.client.deletedIDs) != 0 || exists(filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E03.mkv")) {
		t.Error("a task outside staging was touched")
	}
}

func TestProcessTasksRefusesEscapingPaths(t *testing.T) {
	tu := newTestUpdater(t)
	writeTestFile(t, filepath.Join(tu.stagingDir, "x"), "data")

	tu.ProcessTasks([]Task{stagingTask("dbid_1", "..",
		TaskFile{Filename: "x", Size: 4, SizeDownloaded: 4, Wanted: wantedPtr(true)})})
	tu.ProcessTasks([]Task{stagingTask("dbid_2", "Show.S01",
		TaskFile{Filename: "../../../x", Size: 4, SizeDownloaded: 4, Wanted: wantedPtr(true)})})

	if len(tu.client.deletedIDs) != 0 {
		t.Errorf("deleted = %v", tu.client.deletedIDs)
	}
	if !exists(filepath.Join(tu.stagingDir, "x")) {
		t.Error("file outside the task folder was moved")
	}
}

func TestFinalizeForgetsTaskOfPendingTorrent(t *testing.T) {
	tu := newTestUpdater(t)
	writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E01.mkv"), "1")
	writeTestFile(t, filepath.Join(tu.downloadDir, "Show.S01", "Show.S01E02.mkv"), "2")
	out := tu.HandleTorrent(multiFileTorrent("Show.S01", episodes(3)...), "s.torrent")
	writeTestFile(t, filepath.Join(tu.stagingDir, "Show.S01", "Show.S01E03.mkv"), "3")

	tu.ProcessTasks([]Task{stagingTask("dbid_9", "Show.S01",
		TaskFile{Filename: "Show.S01E03.mkv", Size: 1, SizeDownloaded: 1, Wanted: wantedPtr(true)})})
	tu.client.deletedIDs = nil

	if _, err := tu.DownloadFull(out.PendingID); err != nil {
		t.Fatal(err)
	}
	if len(tu.client.deletedIDs) != 0 {
		t.Errorf("finished task deleted twice: %v", tu.client.deletedIDs)
	}
}

func TestTaskFileIsWanted(t *testing.T) {
	if !(TaskFile{Priority: "normal"}).IsWanted() || (TaskFile{Priority: "skip"}).IsWanted() {
		t.Error("priority fallback is wrong")
	}
	if (TaskFile{Priority: "normal", Wanted: wantedPtr(false)}).IsWanted() {
		t.Error("explicit wanted=false must win")
	}
}

func TestProcessTasksIgnoresEmptyAndUnstartedTasks(t *testing.T) {
	tu := newTestUpdater(t)
	task := seasonWithStagedEpisode(t, tu)
	task.Additional.File = append(task.Additional.File,
		TaskFile{Filename: "empty.nfo", Size: 0, SizeDownloaded: 0, Wanted: wantedPtr(true)})

	tu.ProcessTasks([]Task{stagingTask("dbid_1", "Show.S01")}) // waiting: no file list yet
	if len(tu.client.deletedIDs) != 0 {
		t.Fatal("a task without a file list must be left alone")
	}

	tu.ProcessTasks([]Task{task})
	if fmt.Sprint(tu.client.deletedIDs) != "[dbid_9]" {
		t.Errorf("an empty wanted file blocked finalizing: deleted = %v", tu.client.deletedIDs)
	}
}

func TestProcessTasksReportsTaskStuckWaiting(t *testing.T) {
	tu := newTestUpdater(t)
	now := time.Unix(1_800_000_000, 0)
	tu.now = func() time.Time { return now }
	task := stagingTask("dbid_9", "Show.S01")
	task.Status = "waiting"
	task.Additional.Detail.CreateTime = now.Add(-10 * time.Minute).Unix()

	tu.ProcessTasks([]Task{task})
	if len(tu.notes) != 0 {
		t.Fatalf("reported too early: %v", tu.notes)
	}

	now = now.Add(10 * time.Minute)
	tu.ProcessTasks([]Task{task})
	tu.ProcessTasks([]Task{task})
	if len(tu.notes) != 1 || !strings.Contains(tu.notes[0], "waiting for 20 min") {
		t.Errorf("notes = %v", tu.notes)
	}
	if len(tu.client.deletedIDs) != 0 {
		t.Error("a waiting task must not be deleted")
	}
}
