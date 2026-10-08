// Package backup copies a consistent cold-only snapshot of a data dir.
package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/flush"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/manifest"
)

// ManifestFile is the manifest's file name in a data dir and in a backup.
const ManifestFile = "manifest.sqlite"

// Report describes one backup.
type Report struct {
	FlushedRows int64         `json:"flushed_rows"`
	Files       int           `json:"files"`
	Bytes       int64         `json:"bytes"`
	Duration    time.Duration `json:"duration"`
}

// Backup writes a restorable copy of dataDir into outDir, which must not
// exist or be empty. It holds the maintenance lock for its whole run so flush
// and retention never move or delete files while it copies; the caller must
// not hold it. It flushes every hot hour, snapshots the manifest with
// VACUUM INTO, and copies exactly the files the snapshot lists. Rows that
// arrive after the forced flush stay hot and are not in this backup.
//
// The backup has no hot.duckdb: restoring is opening a store on outDir.
func Backup(ctx context.Context, lock *sync.Mutex, f *flush.Flusher, m *manifest.Manifest, dataDir, outDir string) (Report, error) {
	start := time.Now()
	lock.Lock()
	defer lock.Unlock()

	var rep Report
	if err := ensureEmpty(outDir); err != nil {
		return rep, err
	}

	stats, err := f.FlushAll(ctx)
	if err != nil {
		return rep, fmt.Errorf("force flush: %w", err)
	}
	for _, s := range stats {
		rep.FlushedRows += s.Rows
	}

	snapshotPath := filepath.Join(outDir, ManifestFile)
	vacuum := "VACUUM INTO '" + strings.ReplaceAll(snapshotPath, "'", "''") + "'"
	if _, err := m.DB().ExecContext(ctx, vacuum); err != nil {
		return rep, fmt.Errorf("snapshot manifest: %w", err)
	}
	snapshot, err := manifest.Open(snapshotPath)
	if err != nil {
		return rep, err
	}
	files, err := snapshot.All(ctx)
	closeErr := snapshot.Close()
	if err != nil {
		return rep, err
	}
	if closeErr != nil {
		return rep, fmt.Errorf("close snapshot: %w", closeErr)
	}

	dirs := map[string]bool{outDir: true}
	for _, file := range files {
		dst := filepath.Join(outDir, file.Path)
		n, err := copyFile(filepath.Join(dataDir, file.Path), dst)
		if err != nil {
			return rep, err
		}
		for d := filepath.Dir(dst); d != outDir && d != filepath.Dir(d); d = filepath.Dir(d) {
			dirs[d] = true
		}
		rep.Files++
		rep.Bytes += n
	}
	for d := range dirs {
		if err := syncDir(d); err != nil {
			return rep, err
		}
	}

	rep.Duration = time.Since(start)
	return rep, nil
}

func ensureEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return createDurably(dir)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("backup dir %s is not empty", dir)
	}
	return nil
}

// createDurably creates dir and any missing ancestors, then syncs the parent
// of each created directory so their entries survive a power loss.
func createDurably(dir string) error {
	dir = filepath.Clean(dir)
	existing := dir
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for d := dir; d != existing; d = filepath.Dir(d) {
		if err := syncDir(filepath.Dir(d)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", dst, err)
	}
	n, err := io.Copy(out, in)
	if err != nil {
		_ = out.Close()
		return 0, fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return 0, fmt.Errorf("sync %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return 0, fmt.Errorf("close %s: %w", dst, err)
	}
	return n, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir %s: %w", dir, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return fmt.Errorf("sync dir %s: %w", dir, err)
	}
	return d.Close()
}
