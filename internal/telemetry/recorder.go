package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const DefaultMaxBytes int64 = 16 << 20
const BackupFiles = 3

// FileRecorder writes private local JSONL, fsyncing each event. Rotation keeps
// one current file and three backups. Only one process may own a log path.
type FileRecorder struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	closed   bool
}

func NewRunID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func Open(path string, maxBytes int64) (*FileRecorder, error) {
	if path == "" || maxBytes <= 0 || maxBytes > 1<<30 {
		return nil, fmt.Errorf("event log requires a path and size between 1 byte and 1 GiB")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	r := &FileRecorder{path: path, maxBytes: maxBytes}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *FileRecorder) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	fail := func(err error) error { f.Close(); return err }
	stat, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !stat.Mode().IsRegular() {
		return fail(fmt.Errorf("event log must be a regular file"))
	}
	if err := f.Chmod(0o600); err != nil {
		return fail(err)
	}
	if stat.Size() > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], stat.Size()-1); err != nil {
			return fail(err)
		}
		if last[0] != '\n' {
			return fail(fmt.Errorf("event log ends in an incomplete line; preserve/rename it before restarting"))
		}
	}
	r.file = f
	return nil
}
func (r *FileRecorder) Record(event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("event log is closed")
	}
	if int64(len(data)) > r.maxBytes {
		return fmt.Errorf("event exceeds log size limit")
	}
	if r.file == nil {
		if err := r.open(); err != nil {
			return err
		}
	}
	stat, err := r.file.Stat()
	if err != nil {
		return err
	}
	if stat.Size()+int64(len(data)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			return err
		}
	}
	n, err := r.file.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return r.file.Sync()
}
func (r *FileRecorder) rotate() error {
	if err := r.file.Close(); err != nil {
		r.file = nil
		return err
	}
	r.file = nil
	for i := BackupFiles; i >= 1; i-- {
		src := r.path
		if i > 1 {
			src = fmt.Sprintf("%s.%d", r.path, i-1)
		}
		dst := fmt.Sprintf("%s.%d", r.path, i)
		if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return r.open()
}
func (r *FileRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.file == nil {
		return nil
	}
	err := errors.Join(r.file.Sync(), r.file.Close())
	r.file = nil
	return err
}
