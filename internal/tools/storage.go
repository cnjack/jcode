package tools

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	appconfig "github.com/cnjack/jcode/internal/config"
)

// ---------------------------------------------------------------------------
// StorageManager
// ---------------------------------------------------------------------------

// StorageManager provides persistence under ~/.jcode/storage/.
type StorageManager struct {
	baseDir    string
	sessionID  string
	mu         sync.RWMutex
	writeQueue *WriteQueue
}

// NewStorageManager creates the storage root and all subdirectories.
func NewStorageManager(sessionID string) (*StorageManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(home, ".jcode", "storage")

	subdirs := []string{"file-history", "tool-results", "todos", "plans", "tasks", "oauth"}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o700); err != nil {
			return nil, err
		}
	}

	sm := &StorageManager{
		baseDir:    base,
		sessionID:  sessionID,
		writeQueue: NewWriteQueue(100 * time.Millisecond),
	}
	return sm, nil
}

// Write synchronously writes data to path, creating parent directories.
func (sm *StorageManager) Write(path string, data []byte, mode os.FileMode) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// WriteAsync enqueues a write to be flushed by the WriteQueue.
func (sm *StorageManager) WriteAsync(path string, data []byte, mode os.FileMode) {
	sm.writeQueue.Enqueue(path, WriteEntry{
		Data: data,
		Mode: mode,
	})
}

// AppendAsync enqueues an append to be flushed by the WriteQueue.
func (sm *StorageManager) AppendAsync(path string, data []byte, mode os.FileMode) {
	sm.writeQueue.Enqueue(path, WriteEntry{
		Data:   data,
		Mode:   mode,
		Append: true,
	})
}

// Close drains the write queue.
func (sm *StorageManager) Close() error {
	sm.writeQueue.Close()
	return nil
}

// Cleanup removes old files from the todos and tasks directories,
// keeping the most recent entries by modification time.
func (sm *StorageManager) Cleanup() error {
	var firstErr error
	if err := cleanupDir(sm.TodosDir(), 20); err != nil {
		appconfig.Logger().Printf("storage: cleanup todos failed: %v", err)
		firstErr = err
	}
	if err := cleanupDir(sm.TasksDir(), 50); err != nil {
		appconfig.Logger().Printf("storage: cleanup tasks failed: %v", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// FileHistoryDir returns the file-history directory path.
func (sm *StorageManager) FileHistoryDir() string {
	return filepath.Join(sm.baseDir, "file-history")
}

// ToolResultsDir returns the tool-results directory path.
func (sm *StorageManager) ToolResultsDir() string {
	return filepath.Join(sm.baseDir, "tool-results")
}

// TodosDir returns the todos directory path.
func (sm *StorageManager) TodosDir() string {
	return filepath.Join(sm.baseDir, "todos")
}

// PlansDir returns the plans directory path.
func (sm *StorageManager) PlansDir() string {
	return filepath.Join(sm.baseDir, "plans")
}

// TasksDir returns the tasks directory path.
func (sm *StorageManager) TasksDir() string {
	return filepath.Join(sm.baseDir, "tasks")
}

// OAuthDir returns the oauth directory path.
func (sm *StorageManager) OAuthDir() string {
	return filepath.Join(sm.baseDir, "oauth")
}

// ---------------------------------------------------------------------------
// WriteQueue
// ---------------------------------------------------------------------------

// WriteEntry represents a single pending write or append operation.
type WriteEntry struct {
	Data     []byte
	Mode     os.FileMode
	Append   bool
	Callback func(error)
}

// WriteQueue batches writes and flushes them on a timer or on demand.
type WriteQueue struct {
	entries  map[string][]WriteEntry
	flushCh  chan struct{}
	interval time.Duration
	mu       sync.Mutex
	flushMu  sync.Mutex // serialises drain operations
	done     chan struct{}
}

// NewWriteQueue starts a background drainLoop at the given interval.
func NewWriteQueue(interval time.Duration) *WriteQueue {
	wq := &WriteQueue{
		entries:  make(map[string][]WriteEntry),
		flushCh:  make(chan struct{}, 1),
		interval: interval,
		done:     make(chan struct{}),
	}
	go wq.drainLoop()
	return wq
}

// Enqueue adds an entry for the given path, non-blocking.
func (wq *WriteQueue) Enqueue(path string, entry WriteEntry) {
	wq.mu.Lock()
	wq.entries[path] = append(wq.entries[path], entry)
	wq.mu.Unlock()

	// non-blocking signal
	select {
	case wq.flushCh <- struct{}{}:
	default:
	}
}

// DrainSync synchronously flushes all pending writes.
func (wq *WriteQueue) DrainSync() {
	wq.drain()
}

// Close signals the drainLoop to stop and drains remaining entries.
func (wq *WriteQueue) Close() {
	close(wq.done)
	wq.drain()
}

func (wq *WriteQueue) drainLoop() {
	ticker := time.NewTicker(wq.interval)
	defer ticker.Stop()
	for {
		select {
		case <-wq.done:
			return
		case <-ticker.C:
			wq.drain()
		case <-wq.flushCh:
			wq.drain()
		}
	}
}

func (wq *WriteQueue) drain() {
	wq.flushMu.Lock()
	defer wq.flushMu.Unlock()

	wq.mu.Lock()
	snapshot := wq.entries
	wq.entries = make(map[string][]WriteEntry)
	wq.mu.Unlock()

	for path, entries := range snapshot {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			appconfig.Logger().Printf("storage: mkdir failed for %s: %v", path, err)
			for _, e := range entries {
				if e.Callback != nil {
					e.Callback(err)
				}
			}
			continue
		}
		for _, e := range entries {
			var err error
			if e.Append {
				var f *os.File
				f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, e.Mode)
				if err == nil {
					_, err = f.Write(e.Data)
					_ = f.Close()
				}
			} else {
				err = os.WriteFile(path, e.Data, e.Mode)
			}
			if err != nil {
				appconfig.Logger().Printf("storage: write failed for %s: %v", path, err)
			}
			if e.Callback != nil {
				e.Callback(err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// FileTracker
// ---------------------------------------------------------------------------

// ConflictStatus describes the result of a conflict check.
type ConflictStatus int

const (
	ConflictNone ConflictStatus = iota
	ConflictModified
	ConflictFileGone
)

// ConflictResult holds the details of a conflict check.
type ConflictResult struct {
	Status     ConflictStatus
	OldHash    string
	NewHash    string
	OldModTime time.Time
	NewModTime time.Time
}

// FileState tracks the known state of a single file.
type FileState struct {
	Path        string
	ContentHash string // MD5 hex
	ModTime     time.Time
	Version     int
	BackupPath  string
}

// FileTracker detects external modifications to files the agent has read.
type FileTracker struct {
	tracked  map[string]*FileState
	mu       sync.RWMutex
	storage  *StorageManager
	maxSnaps int
}

// NewFileTracker returns a FileTracker backed by the given StorageManager.
func NewFileTracker(storage *StorageManager) *FileTracker {
	return &FileTracker{
		tracked:  make(map[string]*FileState),
		storage:  storage,
		maxSnaps: 100,
	}
}

// TrackRead records the hash and mod-time of a file that was just read.
func (ft *FileTracker) TrackRead(path string, content []byte, modTime time.Time) {
	h := md5.Sum(content)
	hash := hex.EncodeToString(h[:])

	ft.mu.Lock()
	defer ft.mu.Unlock()

	if st, ok := ft.tracked[path]; ok {
		st.ContentHash = hash
		st.ModTime = modTime
	} else {
		ft.tracked[path] = &FileState{
			Path:        path,
			ContentHash: hash,
			ModTime:     modTime,
			Version:     0,
		}
	}
}

// CheckConflict compares current disk state with the tracked state.
// Returns ConflictNone if path is untracked or unchanged.
func (ft *FileTracker) CheckConflict(path string) (ConflictResult, error) {
	ft.mu.RLock()
	st, ok := ft.tracked[path]
	ft.mu.RUnlock()
	if !ok {
		return ConflictResult{Status: ConflictNone}, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ConflictResult{
				Status:     ConflictFileGone,
				OldHash:    st.ContentHash,
				OldModTime: st.ModTime,
			}, nil
		}
		return ConflictResult{}, err
	}

	// Fast path: if mtime unchanged, no conflict.
	if info.ModTime().Equal(st.ModTime) {
		return ConflictResult{Status: ConflictNone}, nil
	}

	// Mtime changed — compare content hash.
	data, err := os.ReadFile(path)
	if err != nil {
		return ConflictResult{}, err
	}
	h := md5.Sum(data)
	newHash := hex.EncodeToString(h[:])

	if newHash == st.ContentHash {
		// Content identical despite mtime change (e.g. touch).
		return ConflictResult{Status: ConflictNone}, nil
	}

	return ConflictResult{
		Status:     ConflictModified,
		OldHash:    st.ContentHash,
		NewHash:    newHash,
		OldModTime: st.ModTime,
		NewModTime: info.ModTime(),
	}, nil
}

// CreateBackup writes a backup copy into the file-history directory.
// Returns the backup file path.
func (ft *FileTracker) CreateBackup(path string, content []byte) (string, error) {
	ft.mu.Lock()
	st, ok := ft.tracked[path]
	if !ok {
		st = &FileState{Path: path}
		ft.tracked[path] = st
	}
	st.Version++
	version := st.Version
	ft.mu.Unlock()

	h := sha256.Sum256([]byte(path))
	nameHash := hex.EncodeToString(h[:8])
	base := filepath.Base(path)
	backupName := nameHash + "_v" + itoa(version) + "_" + base
	backupPath := filepath.Join(ft.storage.FileHistoryDir(), backupName)

	if err := ft.storage.Write(backupPath, content, 0o600); err != nil {
		return "", err
	}

	ft.mu.Lock()
	st.BackupPath = backupPath
	ft.mu.Unlock()

	// Evict old backups if count exceeds maxSnaps.
	if err := evictOldBackups(ft.storage.FileHistoryDir(), ft.maxSnaps); err != nil {
		appconfig.Logger().Printf("storage: evict old backups failed: %v", err)
	}

	return backupPath, nil
}

// UpdateAfterWrite updates the tracked state for a file that was just written.
func (ft *FileTracker) UpdateAfterWrite(path string, content []byte, modTime time.Time) {
	h := md5.Sum(content)
	hash := hex.EncodeToString(h[:])

	ft.mu.Lock()
	defer ft.mu.Unlock()

	if st, ok := ft.tracked[path]; ok {
		st.ContentHash = hash
		st.ModTime = modTime
	} else {
		ft.tracked[path] = &FileState{
			Path:        path,
			ContentHash: hash,
			ModTime:     modTime,
		}
	}
}

// ---------------------------------------------------------------------------
// Cleanup helpers
// ---------------------------------------------------------------------------

// cleanupDir removes oldest files (by modification time) from dir, keeping at most keepCount.
func cleanupDir(dir string, keepCount int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type fileEntry struct {
		path    string
		modTime time.Time
	}

	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{
			path:    filepath.Join(dir, e.Name()),
			modTime: info.ModTime(),
		})
	}

	if len(files) <= keepCount {
		return nil
	}

	// Sort by modification time, newest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Delete files beyond keepCount.
	var errs []error
	for _, f := range files[keepCount:] {
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to remove %d file(s): %w", len(errs), errs[0])
	}
	return nil
}

// evictOldBackups removes the oldest backup files from dir when count exceeds maxCount.
func evictOldBackups(dir string, maxCount int) error {
	return cleanupDir(dir, maxCount)
}

// itoa is a small helper to avoid importing strconv for a single use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ---------------------------------------------------------------------------
// ToolResultStore
// ---------------------------------------------------------------------------

// PersistedResult describes a tool result that has been spilled to disk.
type PersistedResult struct {
	FilePath     string `json:"filepath"`
	OriginalSize int    `json:"original_size"`
	Preview      string `json:"preview"`
	HasMore      bool   `json:"has_more"`
}

// ToolResultStore persists large tool outputs to disk.
type ToolResultStore struct {
	storage *StorageManager
	maxSize int
}

// NewToolResultStore creates a ToolResultStore with a 50 000 char threshold.
func NewToolResultStore(storage *StorageManager) *ToolResultStore {
	return &ToolResultStore{
		storage: storage,
		maxSize: 50000,
	}
}

// PersistIfLarge writes result to disk if it exceeds the threshold.
// Returns the persisted metadata and true, or zero value and false if small.
func (ts *ToolResultStore) PersistIfLarge(toolName, result string) (PersistedResult, bool) {
	if len(result) < ts.maxSize {
		return PersistedResult{}, false
	}

	h := sha256.Sum256([]byte(result))
	name := toolName + "_" + hex.EncodeToString(h[:8]) + ".txt"
	path := filepath.Join(ts.storage.ToolResultsDir(), name)

	if err := ts.storage.Write(path, []byte(result), 0o600); err != nil {
		appconfig.Logger().Printf("storage: persist result failed: %v", err)
		return PersistedResult{}, false
	}

	preview := result
	if len(preview) > 500 {
		preview = preview[:500]
	}

	return PersistedResult{
		FilePath:     path,
		OriginalSize: len(result),
		Preview:      preview,
		HasMore:      true,
	}, true
}

// Retrieve reads a previously-persisted result from disk.
func (ts *ToolResultStore) Retrieve(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ---------------------------------------------------------------------------
// TokenStore (MCP OAuth)
// ---------------------------------------------------------------------------

// OAuthToken represents an OAuth token for an MCP provider.
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	Provider     string    `json:"provider"`
}

// TokenStore persists OAuth tokens as JSON files.
type TokenStore struct {
	dir string
}

// NewTokenStore returns a TokenStore that reads/writes in dir.
func NewTokenStore(dir string) *TokenStore {
	return &TokenStore{dir: dir}
}

// Save writes the token for a provider as JSON with 0600 permissions.
func (ts *TokenStore) Save(provider string, token OAuthToken) error {
	if err := os.MkdirAll(ts.dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(ts.dir, provider+".json"), data, 0o600)
}

// Get reads the token for a provider. Returns nil, nil if not found.
func (ts *TokenStore) Get(provider string) (*OAuthToken, error) {
	data, err := os.ReadFile(filepath.Join(ts.dir, provider+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var token OAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// Delete removes the token file for a provider.
func (ts *TokenStore) Delete(provider string) error {
	err := os.Remove(filepath.Join(ts.dir, provider+".json"))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// TaskLog (background tasks)
// ---------------------------------------------------------------------------

// TaskLog writes output from a background task to disk.
type TaskLog struct {
	taskID  string
	file    *os.File
	maxSize int64
	written int64
	mu      sync.Mutex
}

// NewTaskLog opens (or creates) a log file for the task in the tasks dir.
func NewTaskLog(storage *StorageManager, taskID string) (*TaskLog, error) {
	path := filepath.Join(storage.TasksDir(), taskID+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &TaskLog{
		taskID:  taskID,
		file:    f,
		maxSize: 64 * 1024 * 1024, // 64 MB
		written: info.Size(),
	}, nil
}

// Write appends data to the task log, respecting the size limit.
func (tl *TaskLog) Write(data []byte) (int, error) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	remaining := tl.maxSize - tl.written
	if remaining <= 0 {
		return 0, nil
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
	}
	n, err := tl.file.Write(data)
	tl.written += int64(n)
	return n, err
}

// ReadAll reads the entire task log from disk.
func (tl *TaskLog) ReadAll() (string, error) {
	tl.mu.Lock()
	path := tl.file.Name()
	tl.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Close closes the underlying file.
func (tl *TaskLog) Close() error {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.file.Close()
}
