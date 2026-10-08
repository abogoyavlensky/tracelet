package flush

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/manifest"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
)

// StepRetentionRemoved is the hook step after a retention file removal and
// before its manifest row is deleted.
const StepRetentionRemoved = "retention-removed"

// Policy is a retention policy.
type Policy struct {
	// Days keeps, per signal, the hours whose end is within that many days of
	// now. A signal without an entry is kept by age.
	Days map[string]int
	// MaxBytes caps the cold files' total size; the oldest hours across all
	// signals go first. Zero means no cap.
	MaxBytes int64
}

// RetentionReport describes one retention run.
type RetentionReport struct {
	HoursDeleted int   `json:"hours_deleted"`
	FilesDeleted int   `json:"files_deleted"`
	BytesFreed   int64 `json:"bytes_freed"`
}

// ApplyRetention deletes cold files by age per signal, then the oldest hours
// across signals while the total exceeds MaxBytes. Each file is removed from
// disk before its manifest row, so a crash in between leaves a row for a
// missing file, which Reconcile drops: an interrupted deletion completes on
// the next start and nothing is resurrected.
func (f *Flusher) ApplyRetention(ctx context.Context, p Policy, now time.Time) (RetentionReport, error) {
	var rep RetentionReport

	for _, signal := range store.Signals {
		days, ok := p.Days[signal]
		if !ok {
			continue
		}
		keepAfter := now.Add(-time.Duration(days) * 24 * time.Hour)
		hours, err := f.Manifest.Hours(ctx, signal)
		if err != nil {
			return rep, err
		}
		for _, hour := range hours {
			if hour.Add(time.Hour).After(keepAfter) {
				break
			}
			files, err := f.Manifest.HourFiles(ctx, hour)
			if err != nil {
				return rep, err
			}
			var ofSignal []manifest.File
			for _, file := range files {
				if file.Signal == signal {
					ofSignal = append(ofSignal, file)
				}
			}
			if err := f.removeFiles(ctx, ofSignal, &rep); err != nil {
				return rep, err
			}
			rep.HoursDeleted++
		}
	}

	if p.MaxBytes <= 0 {
		return rep, nil
	}
	for {
		total, err := f.Manifest.TotalBytes(ctx)
		if err != nil {
			return rep, err
		}
		if total <= p.MaxBytes {
			return rep, nil
		}
		hour, _, ok, err := f.Manifest.OldestHour(ctx)
		if err != nil || !ok {
			return rep, err
		}
		files, err := f.Manifest.HourFiles(ctx, hour)
		if err != nil {
			return rep, err
		}
		if err := f.removeFiles(ctx, files, &rep); err != nil {
			return rep, err
		}
		rep.HoursDeleted++
	}
}

func (f *Flusher) removeFiles(ctx context.Context, files []manifest.File, rep *RetentionReport) error {
	for _, file := range files {
		path := filepath.Join(f.DataDir, file.Path)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", file.Path, err)
		}
		// Make the unlink durable before the row goes; otherwise a power loss
		// could bring the file back without its row and Reconcile would adopt it.
		if err := syncDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := f.hook(StepRetentionRemoved); err != nil {
			return fmt.Errorf("retention %s: %w", file.Path, err)
		}
		if err := f.Manifest.Remove(ctx, file.Path); err != nil {
			return err
		}
		removeEmptyParents(f.DataDir, filepath.Dir(file.Path))
		rep.FilesDeleted++
		rep.BytesFreed += file.Bytes
	}
	return nil
}

// removeEmptyParents removes the hour= and date= directories of a file once
// they are empty. Failures are ignored: a non-empty directory stays, and an
// empty one left behind costs nothing.
func removeEmptyParents(dataDir, relDir string) {
	for d := relDir; strings.Contains(filepath.Base(d), "="); d = filepath.Dir(d) {
		if err := os.Remove(filepath.Join(dataDir, d)); err != nil {
			return
		}
	}
}

// DiskFree returns the bytes available to unprivileged users on dir's
// filesystem.
func DiskFree(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	return int64(st.Bavail) * st.Bsize, nil
}

// DirBytes returns the disk usage, in allocated bytes, of each top-level
// entry of dir.
func DirBytes(dir string) (map[string]int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := map[string]int64{}
	for _, e := range entries {
		var n int64
		err := filepath.WalkDir(filepath.Join(dir, e.Name()), func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil // removed while walking
				}
				return err
			}
			info, err := d.Info()
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			n += allocated(info)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", e.Name(), err)
		}
		out[e.Name()] = n
	}
	return out, nil
}

func allocated(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return info.Size()
}
