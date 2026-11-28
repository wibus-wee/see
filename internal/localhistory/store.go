package localhistory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"see/internal/fileobs"
)

const (
	maxTextBytes        = 1 << 20
	maxRecentVersions   = 100
	maxCoarseVersions   = 128
	maxRetainedVersions = maxRecentVersions + maxCoarseVersions
	maxAge              = 7 * 24 * time.Hour
	maxStoreBytes       = 128 << 20
	maxStoreFiles       = 512
	recentWindow        = 15 * time.Minute
	recentInterval      = 30 * time.Second
	middleWindow        = 2 * time.Hour
	middleInterval      = 5 * time.Minute
	dayWindow           = 24 * time.Hour
	dayInterval         = 30 * time.Minute
	oldInterval         = 6 * time.Hour
)

const MaxTextBytes = maxTextBytes

var storeMu sync.Mutex

type Snapshot struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Text      string    `json:"text"`
	// Digest is the full hex SHA-256 of Text, measured when the snapshot
	// is written. Records written before digests existed carry an empty
	// digest and stay unverifiable — they load as before rather than
	// pretending an integrity check that was never measured.
	Digest string `json:"digest,omitempty"`
	// Corrupt is set at read time when the stored text no longer matches
	// its digest: semantic corruption the JSON shape cannot reveal. A
	// corrupt snapshot stays listed so the user sees when it was taken,
	// but its text is dropped and it must never be restored.
	Corrupt bool `json:"-"`
}

type fileHistory struct {
	Path      string     `json:"path"`
	Snapshots []Snapshot `json:"snapshots"`
}

type Store struct{ dir string }

func New(dir string) *Store { return &Store{dir: dir} }

func CacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "See", "localhistory"), nil
}

func NewCacheStore() (*Store, error) {
	dir, err := CacheDir()
	if err != nil {
		return nil, err
	}
	return New(dir), nil
}

func (s *Store) Add(path, text string, at time.Time) (Snapshot, bool, error) {
	if len(text) > maxTextBytes || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return Snapshot{}, false, errors.New("local history only stores UTF-8 text files up to 1 MiB")
	}
	canonical, err := canonicalPath(path)
	if err != nil {
		return Snapshot{}, false, err
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := s.ensureDir(); err != nil {
		return Snapshot{}, false, err
	}
	name := s.filePath(canonical)
	record, err := readHistory(name, canonical)
	if err != nil {
		return Snapshot{}, false, err
	}
	if record == nil {
		record = &fileHistory{Path: canonical}
	}
	cutoff := time.Now().Add(-maxAge)
	oldCount := len(record.Snapshots)
	record.Snapshots = slices.DeleteFunc(record.Snapshots, func(v Snapshot) bool { return v.CreatedAt.Before(cutoff) })
	for _, old := range record.Snapshots {
		if old.Text == text {
			if len(record.Snapshots) != oldCount {
				if err := writeHistory(name, record); err != nil {
					return Snapshot{}, false, err
				}
				if err := s.trimGlobal(canonical, cutoff); err != nil {
					return Snapshot{}, false, err
				}
			}
			return old, false, nil
		}
	}
	digest, err := fileobs.DigestSHA256(strings.NewReader(text))
	if err != nil {
		return Snapshot{}, false, err
	}
	snap := Snapshot{ID: snapshotID(canonical, at, text), CreatedAt: at, Text: text, Digest: fileobs.HexDigest(digest)}
	record.Snapshots = append(record.Snapshots, snap)
	record.Snapshots = retainSnapshots(record.Snapshots)
	if err := writeHistory(name, record); err != nil {
		return Snapshot{}, false, err
	}
	if err := s.trimGlobal(canonical, cutoff); err != nil {
		return Snapshot{}, false, err
	}
	return snap, true, nil
}

func (s *Store) List(path string) ([]Snapshot, error) {
	canonical, err := canonicalPath(path)
	if err != nil {
		return nil, err
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	record, err := readHistory(s.filePath(canonical), canonical)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil || record == nil {
		return nil, err
	}
	cutoff := time.Now().Add(-maxAge)
	out := make([]Snapshot, 0, len(record.Snapshots))
	for _, snap := range record.Snapshots {
		if snap.CreatedAt.Before(cutoff) {
			continue
		}
		if corruptSnap(snap) {
			// The digest no longer matches the stored text. Keep the
			// entry visible with its identity and timestamp, but drop
			// the bytes so nothing downstream can restore them.
			snap.Text = ""
			snap.Corrupt = true
		}
		out = append(out, snap)
	}
	sortSnapshots(out)
	return out, nil
}

// corruptSnap reports whether a digest-bearing snapshot's text fails its
// integrity check. An empty digest means the record predates digests and
// is honestly unverifiable, not corrupt; a malformed digest fails closed
// because a corrupt manifest must never be trusted either.
func corruptSnap(snap Snapshot) bool {
	if snap.Digest == "" {
		return false
	}
	want, err := fileobs.ParseHexDigest(snap.Digest)
	if err != nil {
		return true
	}
	return fileobs.VerifyContentDigest([]byte(snap.Text), want) != nil
}

func (s *Store) ensureDir() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(s.dir, 0o700)
}

func (s *Store) filePath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

func (s *Store) trimGlobal(keepPath string, cutoff time.Time) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	type stored struct {
		name   string
		record *fileHistory
	}
	var files []stored
	var bytes int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		var record fileHistory
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}
		if len(record.Snapshots) == 0 {
			_ = os.Remove(name)
			continue
		}
		oldCount := len(record.Snapshots)
		record.Snapshots = slices.DeleteFunc(record.Snapshots, func(v Snapshot) bool { return v.CreatedAt.Before(cutoff) })
		if len(record.Snapshots) == 0 {
			_ = os.Remove(name)
			continue
		}
		if len(record.Snapshots) != oldCount {
			if err := writeHistory(name, &record); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		bytes += int64(len(encoded))
		files = append(files, stored{name, &record})
	}
	if bytes <= maxStoreBytes && len(files) <= maxStoreFiles {
		return nil
	}
	for len(files) > 0 && (bytes > maxStoreBytes || len(files) > maxStoreFiles) {
		idx := -1
		for i, f := range files {
			if len(files) > maxStoreFiles && f.record.Path != keepPath {
				if idx < 0 || latest(f.record).CreatedAt.Before(latest(files[idx].record).CreatedAt) {
					idx = i
				}
			}
		}
		if idx >= 0 {
			f := files[idx]
			encoded, err := json.Marshal(f.record)
			if err != nil {
				return err
			}
			if err := os.Remove(f.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			bytes -= int64(len(encoded))
			files = slices.Delete(files, idx, idx+1)
			continue
		}
		oldestFile, oldestSnap := -1, -1
		for i, f := range files {
			if f.record.Path == keepPath && len(files) > 1 {
				continue
			}
			candidate := len(f.record.Snapshots) - 1
			if oldestFile < 0 || f.record.Snapshots[candidate].CreatedAt.Before(files[oldestFile].record.Snapshots[oldestSnap].CreatedAt) {
				oldestFile, oldestSnap = i, candidate
			}
		}
		if oldestFile < 0 {
			return nil
		}
		f := files[oldestFile]
		oldSize, err := json.Marshal(f.record)
		if err != nil {
			return err
		}
		f.record.Snapshots = slices.Delete(f.record.Snapshots, oldestSnap, oldestSnap+1)
		if len(f.record.Snapshots) == 0 {
			if err := os.Remove(f.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			files = slices.Delete(files, oldestFile, oldestFile+1)
			bytes -= int64(len(oldSize))
		} else {
			newSize, err := json.Marshal(f.record)
			if err != nil {
				return err
			}
			if err := writeHistory(f.name, f.record); err != nil {
				return err
			}
			bytes += int64(len(newSize) - len(oldSize))
		}
	}
	return nil
}

func latest(record *fileHistory) Snapshot { return record.Snapshots[0] }

func sortSnapshots(snapshots []Snapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		if snapshots[i].CreatedAt.Equal(snapshots[j].CreatedAt) {
			return snapshots[i].ID < snapshots[j].ID
		}
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})
}

type checkpointBucket struct {
	tier  int
	index int64
}

func retainSnapshots(snapshots []Snapshot) []Snapshot {
	sortSnapshots(snapshots)
	if len(snapshots) <= maxRecentVersions {
		return snapshots
	}
	latest := snapshots[0].CreatedAt
	retained := slices.Clone(snapshots[:maxRecentVersions])
	buckets := make(map[checkpointBucket]struct{}, maxCoarseVersions)
	for _, snapshot := range snapshots[maxRecentVersions:] {
		age := latest.Sub(snapshot.CreatedAt)
		tier, interval := checkpointInterval(age)
		if interval == 0 {
			continue
		}
		bucket := checkpointBucket{tier: tier, index: snapshot.CreatedAt.UnixNano() / int64(interval)}
		if _, exists := buckets[bucket]; exists {
			continue
		}
		buckets[bucket] = struct{}{}
		retained = append(retained, snapshot)
		if len(retained) == maxRetainedVersions {
			break
		}
	}
	sortSnapshots(retained)
	return retained
}

func checkpointInterval(age time.Duration) (int, time.Duration) {
	switch {
	case age < recentWindow:
		return 0, recentInterval
	case age < middleWindow:
		return 1, middleInterval
	case age < dayWindow:
		return 2, dayInterval
	case age < maxAge:
		return 3, oldInterval
	default:
		return 0, 0
	}
}

func readHistory(name, path string) (*fileHistory, error) {
	data, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record fileHistory
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("read local history: %w", err)
	}
	if record.Path != path {
		return nil, errors.New("local history path does not match its index")
	}
	return &record, nil
}

func writeHistory(name string, record *fileHistory) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	dir := filepath.Dir(name)
	tmp, err := os.CreateTemp(dir, ".local-history-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, name)
}

func canonicalPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("local history requires a file path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err == nil {
		return filepath.Join(parent, filepath.Base(abs)), nil
	}
	return filepath.Clean(abs), nil
}

func snapshotID(path string, at time.Time, text string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(at.Format(time.RFC3339Nano)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil)[:16])
}
