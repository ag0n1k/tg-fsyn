package main

// Season updates.
//
// Rutracker-style season packs are re-released every week with one more
// episode, under a new infohash. Dropped into the watch folder, each update
// becomes a new DownloadStation task that hash-checks the whole season
// already on disk (tens of minutes on the NAS disks) and re-downloads every
// episode the releaser replaced in the meantime. When a .torrent arrives for
// a folder that already exists in the download dir, SeasonUpdater instead
// queues only the files that are not on disk yet, into a staging dir where
// nothing pre-exists (so there is nothing to hash-check), and moves them into
// the season folder once they are complete.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// FullDownloadCallbackPrefix + pending ID is the callback data of the
	// "download everything" button.
	FullDownloadCallbackPrefix = "full:"
	// DefaultStagingDirName is created inside the download dir.
	DefaultStagingDirName = ".tg-fsyn-staging"
	maxPendingTorrents    = 20
	maxListedFiles        = 15
	// stuckWaitingAfter: a staging task still not started after this long
	// is reported once, so a selective download never hangs silently.
	stuckWaitingAfter = 15 * time.Minute
)

var errPendingExpired = errors.New("this torrent is no longer remembered (the bot was restarted?) — send the .torrent again")

// UpdatePlan splits a multi-file torrent into files already on disk and
// files still missing.
type UpdatePlan struct {
	Name    string
	Present []TorrentFile
	Missing []TorrentFile
}

// PlanSeasonUpdate returns nil when the torrent is not an update of a folder
// that already exists in downloadDir with at least one of its files.
// A file counts as present if a regular file with its name exists — the size
// is not compared, so an episode the releaser replaced is kept as it is.
// DownloadStation's half-downloaded "<name>.part" files do not count.
func PlanSeasonUpdate(meta *TorrentMeta, downloadDir string) (*UpdatePlan, error) {
	if !meta.MultiFile() {
		return nil, nil
	}
	dir := filepath.Join(downloadDir, meta.Name)
	st, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, nil
	}

	plan := &UpdatePlan{Name: meta.Name}
	for _, f := range meta.Files {
		st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f.Path)))
		switch {
		case err == nil && st.Mode().IsRegular():
			plan.Present = append(plan.Present, f)
		case err == nil || errors.Is(err, fs.ErrNotExist):
			plan.Missing = append(plan.Missing, f)
		default:
			return nil, err
		}
	}
	if len(plan.Present) == 0 {
		return nil, nil
	}
	return plan, nil
}

// TorrentOutcome tells the bot how a received .torrent was dealt with.
type TorrentOutcome struct {
	// Handled is false when the torrent is not a season update; the bot then
	// drops it into the watch folder as before.
	Handled bool
	Text    string
	// PendingID, when set, is offered as a "download everything" button.
	PendingID string
}

type pendingTorrent struct {
	fileName string
	data     []byte
	name     string // torrent root folder
	taskID   string // the selective task, while it exists
}

// SeasonUpdater queues season updates selectively and finishes them.
type SeasonUpdater struct {
	synology    SynologyClient
	downloadDir string // where DownloadStation puts torrents by default
	stagingDir  string // selective downloads land here first
	stagingDest string // stagingDir as a DownloadStation destination
	watchDir    string // DownloadStation watch folder for whole torrents

	// notify reaches the admins; indexFile adds a moved file to the media
	// index. Both are optional.
	notify    func(text string)
	indexFile func(path string) error
	now       func() time.Time

	mu      sync.Mutex
	pending map[string]*pendingTorrent
	order   []string

	finalizeMu sync.Mutex
	reported   map[string]bool // task IDs whose finalize problem was already reported
}

// NewSeasonUpdater validates the directories. stagingDir must be on the same
// volume share as downloadDir: finished files are moved with a rename.
func NewSeasonUpdater(synology SynologyClient, downloadDir, stagingDir, watchDir string) (*SeasonUpdater, error) {
	dest, err := dsDestination(stagingDir)
	if err != nil {
		return nil, err
	}
	if _, err := dsDestination(downloadDir); err != nil {
		return nil, err
	}
	// Each share is its own btrfs subvolume, and rename(2) between them
	// fails with EXDEV.
	if shareRoot(stagingDir) != shareRoot(downloadDir) {
		return nil, fmt.Errorf("staging dir %s must be on the same share as %s", stagingDir, downloadDir)
	}
	return &SeasonUpdater{
		synology:    synology,
		downloadDir: filepath.Clean(downloadDir),
		stagingDir:  filepath.Clean(stagingDir),
		stagingDest: dest,
		watchDir:    watchDir,
		pending:     make(map[string]*pendingTorrent),
		reported:    make(map[string]bool),
		now:         time.Now,
	}, nil
}

// dsDestination turns an absolute path on a volume into the share-relative
// form DownloadStation uses: /volume1/video/x → video/x.
func dsDestination(abs string) (string, error) {
	clean := filepath.Clean(abs)
	vol, rest, ok := strings.Cut(strings.TrimPrefix(clean, "/"), "/")
	if !filepath.IsAbs(clean) || !ok || !strings.HasPrefix(vol, "volume") || rest == "" {
		return "", fmt.Errorf("%s is not a folder inside a /volumeN share", abs)
	}
	return rest, nil
}

// shareRoot returns "/volumeN/share" of a path already accepted by
// dsDestination.
func shareRoot(abs string) string {
	parts := strings.SplitN(filepath.Clean(abs), "/", 4)
	return strings.Join(parts[:3], "/")
}

// HandleTorrent decides what to do with a received .torrent.
func (u *SeasonUpdater) HandleTorrent(data []byte, fileName string) TorrentOutcome {
	meta, err := ParseTorrent(data)
	if err != nil {
		log.Printf("Not inspecting %s: %v", fileName, err)
		return TorrentOutcome{}
	}
	plan, err := PlanSeasonUpdate(meta, u.downloadDir)
	if err != nil {
		log.Printf("Cannot compare %s with %s: %v", meta.Name, u.downloadDir, err)
		return TorrentOutcome{}
	}
	if plan == nil {
		return TorrentOutcome{}
	}

	p := &pendingTorrent{fileName: fileName, data: data, name: meta.Name}
	total := len(plan.Present) + len(plan.Missing)
	if len(plan.Missing) == 0 {
		return TorrentOutcome{
			Handled:   true,
			PendingID: u.remember(p),
			Text:      fmt.Sprintf("📺 %s\nAll %d files are already on disk — nothing to download.", meta.Name, total),
		}
	}

	wanted := make(map[string]bool, len(plan.Missing))
	var size int64
	for _, f := range plan.Missing {
		wanted[meta.Name+"/"+f.Path] = true
		size += f.Length
	}
	if err := os.MkdirAll(u.stagingDir, 0o777); err != nil {
		log.Printf("Cannot create staging dir: %v", err)
		return TorrentOutcome{
			Handled:   true,
			PendingID: u.remember(p),
			Text:      fmt.Sprintf("⚠️ %s is an update of a season already on disk, but the staging folder is unusable: %v\nNothing was queued.", meta.Name, err),
		}
	}
	taskID, err := u.synology.AddTorrentSelective(data, fileName, u.stagingDest, wanted)
	if err != nil {
		log.Printf("Selective add of %s failed: %v", meta.Name, err)
		return TorrentOutcome{
			Handled:   true,
			PendingID: u.remember(p),
			Text:      fmt.Sprintf("⚠️ %s is an update of a season already on disk, but queuing only the new files failed: %v\nNothing was queued.", meta.Name, err),
		}
	}
	log.Printf("Season update %s: queued %d of %d files as %s", meta.Name, len(plan.Missing), total, taskID)
	p.taskID = taskID

	var b strings.Builder
	fmt.Fprintf(&b, "🔄 Season update: %s\n", meta.Name)
	fmt.Fprintf(&b, "Already on disk: %d of %d files.\n", len(plan.Present), total)
	fmt.Fprintf(&b, "Downloading only the new ones (%d, %.2f GB); they will be moved into %s when complete:\n",
		len(plan.Missing), float64(size)/(1<<30), filepath.Join(u.downloadDir, meta.Name))
	writeFileList(&b, missingPaths(plan.Missing))
	return TorrentOutcome{Handled: true, PendingID: u.remember(p), Text: b.String()}
}

// DownloadFull queues the whole remembered torrent through the watch folder,
// cancelling its selective download first. Returns the torrent name.
func (u *SeasonUpdater) DownloadFull(id string) (string, error) {
	u.mu.Lock()
	p := u.pending[id]
	u.mu.Unlock()
	if p == nil {
		return "", errPendingExpired
	}

	u.finalizeMu.Lock()
	defer u.finalizeMu.Unlock()
	u.mu.Lock()
	taskID := p.taskID
	u.mu.Unlock()
	if taskID != "" {
		if err := u.synology.DeleteTasks([]string{taskID}); err != nil {
			return "", fmt.Errorf("could not cancel the selective download: %w", err)
		}
		if err := os.RemoveAll(filepath.Join(u.stagingDir, p.name)); err != nil {
			log.Printf("Failed to clear staging for %s: %v", p.name, err)
		}
	}

	path := filepath.Join(u.watchDir, filepath.Base(p.fileName))
	if err := os.WriteFile(path, p.data, 0o644); err != nil {
		return "", fmt.Errorf("could not save the torrent: %w", err)
	}
	log.Printf("File saved: %s (full download of %s)", path, p.name)
	u.forget(id)
	return p.name, nil
}

// OwnsTask reports whether the task downloads into the staging dir.
// Such tasks must be left alone by /cleanup until their files are moved.
func (u *SeasonUpdater) OwnsTask(t Task) bool {
	return t.Additional.Detail.Destination == u.stagingDest
}

// ProcessTasks moves the files of completed staging tasks into their season
// folders, then deletes the task and its staging folder. It is idempotent:
// a file already moved by an interrupted run is recognised and not reported
// as missing.
func (u *SeasonUpdater) ProcessTasks(tasks []Task) {
	u.finalizeMu.Lock()
	defer u.finalizeMu.Unlock()
	for _, t := range tasks {
		if u.OwnsTask(t) {
			u.finalize(t)
		}
	}
}

func (u *SeasonUpdater) finalize(t Task) {
	if err := checkPathComponent(t.Title); err != nil {
		log.Printf("Staging task %s has an unusable title %q: %v", t.ID, t.Title, err)
		return
	}
	d := t.Additional.Detail
	if t.Status == "waiting" && d.StartedTime == 0 && d.CreateTime > 0 {
		waited := u.now().Sub(time.Unix(d.CreateTime, 0))
		if key := t.ID + ":waiting"; waited > stuckWaitingAfter && !u.reported[key] {
			u.reported[key] = true
			log.Printf("Season update %s (%s) has not started for %v", t.Title, t.ID, waited.Round(time.Minute))
			u.send(fmt.Sprintf("⏳ Season update %s has been waiting for %d min without starting. Check DownloadStation (task queue limit, or the bot's DSM account not being allowed to start tasks). The \"Download everything instead\" button under the original message falls back to the watch folder.",
				t.Title, int(waited.Minutes())))
		}
		return
	}

	// Empty files carry no data and may never appear on disk; skip them.
	// A task that has not started yet reports no files at all.
	var wanted []TaskFile
	for _, f := range t.Additional.File {
		if f.IsWanted() && f.Size > 0 {
			wanted = append(wanted, f)
		}
	}
	if len(wanted) == 0 {
		return
	}
	for _, f := range wanted {
		if f.SizeDownloaded < f.Size {
			return
		}
	}

	srcRoot := filepath.Join(u.stagingDir, t.Title)
	dstRoot := filepath.Join(u.downloadDir, t.Title)
	var moved, problems []string
	for _, f := range wanted {
		rel, err := safeRelPath(f.Filename)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", f.Filename, err))
			continue
		}
		src := filepath.Join(srcRoot, rel)
		dst := filepath.Join(dstRoot, rel)
		if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) {
			if _, err := os.Lstat(src + ".part"); err == nil {
				return // DownloadStation has not renamed it yet; next poll
			}
			if _, err := os.Lstat(dst); err == nil {
				moved = append(moved, f.Filename) // moved by an interrupted run
				continue
			}
			problems = append(problems, f.Filename+": not found in the staging folder")
			continue
		}
		if _, err := os.Lstat(dst); err == nil {
			problems = append(problems, f.Filename+": already exists in the season folder, not overwritten")
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o777); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", f.Filename, err))
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", f.Filename, err))
			continue
		}
		moved = append(moved, f.Filename)
		if u.indexFile != nil {
			if err := u.indexFile(dst); err != nil {
				log.Printf("Indexing %s failed: %v", dst, err)
			}
		}
	}

	if len(problems) > 0 {
		log.Printf("Season update %s (%s) needs attention: %s", t.Title, t.ID, strings.Join(problems, "; "))
		if !u.reported[t.ID] {
			u.reported[t.ID] = true
			var b strings.Builder
			fmt.Fprintf(&b, "⚠️ Season update %s: could not move everything into %s. The task and %s are kept for a manual look.\n", t.Title, dstRoot, srcRoot)
			writeFileList(&b, problems)
			u.send(b.String())
		}
		return
	}

	// Moves are done; on failure the next poll retries and finds them done.
	if err := u.synology.DeleteTasks([]string{t.ID}); err != nil {
		log.Printf("Failed to delete finished staging task %s: %v", t.ID, err)
		return
	}
	if err := os.RemoveAll(srcRoot); err != nil {
		log.Printf("Failed to clear staging folder %s: %v", srcRoot, err)
	}
	u.mu.Lock()
	for _, p := range u.pending {
		if p.taskID == t.ID {
			p.taskID = ""
		}
	}
	u.mu.Unlock()
	delete(u.reported, t.ID)
	delete(u.reported, t.ID+":waiting")
	log.Printf("Season update %s: moved %d file(s) into %s", t.Title, len(moved), dstRoot)

	var b strings.Builder
	fmt.Fprintf(&b, "✅ Season update %s: moved %d new file(s) into %s\n", t.Title, len(moved), dstRoot)
	writeFileList(&b, moved)
	u.send(b.String())
}

func (u *SeasonUpdater) send(text string) {
	if u.notify != nil {
		u.notify(text)
	}
}

func (u *SeasonUpdater) remember(p *pendingTorrent) string {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	id := hex.EncodeToString(raw[:])
	u.mu.Lock()
	defer u.mu.Unlock()
	u.pending[id] = p
	u.order = append(u.order, id)
	for len(u.order) > maxPendingTorrents {
		delete(u.pending, u.order[0])
		u.order = u.order[1:]
	}
	return id
}

func (u *SeasonUpdater) forget(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.pending, id)
	for i, o := range u.order {
		if o == id {
			u.order = append(u.order[:i], u.order[i+1:]...)
			break
		}
	}
}

// safeRelPath validates a slash-separated path from DownloadStation and
// returns it in OS form, refusing anything that could leave its base dir.
func safeRelPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	for _, part := range strings.Split(p, "/") {
		if err := checkPathComponent(part); err != nil {
			return "", err
		}
	}
	return filepath.FromSlash(p), nil
}

func missingPaths(files []TorrentFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func writeFileList(b *strings.Builder, items []string) {
	for i, it := range items {
		if i >= maxListedFiles {
			fmt.Fprintf(b, "…and %d more\n", len(items)-i)
			break
		}
		fmt.Fprintf(b, "• %s\n", it)
	}
}
