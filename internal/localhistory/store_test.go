package localhistory

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStoreAddsDeduplicatedNewestFirstSnapshots(t *testing.T) {
	store := New(t.TempDir())
	path := filepath.Join(t.TempDir(), "source.go")
	first := time.Now().UTC()
	if _, added, err := store.Add(path, "first\n", first); err != nil || !added {
		t.Fatalf("add first: added=%v err=%v", added, err)
	}
	if _, added, err := store.Add(path, "first\n", first.Add(time.Second)); err != nil || added {
		t.Fatalf("deduplicate: added=%v err=%v", added, err)
	}
	if _, added, err := store.Add(path, "second\n", first.Add(2*time.Second)); err != nil || !added {
		t.Fatalf("add second: added=%v err=%v", added, err)
	}
	got, err := store.List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "second\n" || got[1].Text != "first\n" || !got[0].CreatedAt.After(got[1].CreatedAt) {
		t.Fatalf("snapshots = %+v", got)
	}
}

func TestStoreUsesPrivatePermissionsAndPersistsAcrossInstances(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	path := filepath.Join(t.TempDir(), "source.go")
	store := New(dir)
	if _, _, err := store.Add(path, "private", time.Now()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("cache directory permissions = %04o, want 0700", got)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("stored files = %v, err=%v", entries, err)
	}
	info, err = os.Stat(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("snapshot permissions = %04o, want 0600", got)
		}
	}
	reopened := New(dir)
	got, err := reopened.List(path)
	if err != nil || len(got) != 1 || got[0].Text != "private" {
		t.Fatalf("reopened snapshots = %+v, err=%v", got, err)
	}
}

func TestStoreRetentionAndRejectedText(t *testing.T) {
	store := New(t.TempDir())
	path := filepath.Join(t.TempDir(), "source.go")
	first := time.Now().UTC()
	for i := 0; i < maxRecentVersions+1; i++ {
		if _, _, err := store.Add(path, strings.Repeat("x", i+1), first.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxRecentVersions+1 || got[0].Text != strings.Repeat("x", maxRecentVersions+1) || got[len(got)-1].Text != "x" {
		t.Fatalf("retained %d snapshots, expected recent plus an old checkpoint: %+v", len(got), got)
	}
	if _, _, err := store.Add(path, strings.Repeat("x", maxTextBytes+1), first.Add(time.Hour)); err == nil {
		t.Fatal("oversize text was accepted")
	}
	if _, _, err := store.Add(path, "not text\x00", first.Add(time.Hour)); err == nil {
		t.Fatal("binary text was accepted")
	}
}

func TestStoreKeepsThirtySecondCheckpointsThroughDenseEdits(t *testing.T) {
	store := New(t.TempDir())
	path := filepath.Join(t.TempDir(), "source.go")
	const edits = 501
	const pause = 1500 * time.Millisecond
	latest := time.Now().UTC()
	first := latest.Add(-time.Duration(edits-1) * pause)
	for i := 0; i < edits; i++ {
		if _, _, err := store.Add(path, fmt.Sprintf("revision-%03d", i), first.Add(time.Duration(i)*pause)); err != nil {
			t.Fatal(err)
		}
	}
	snapshots, err := store.List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) <= maxRecentVersions || len(snapshots) >= edits || len(snapshots) > maxRetainedVersions {
		t.Fatalf("retained %d of %d dense snapshots, want recent detail and bounded checkpoints", len(snapshots), edits)
	}
	represented := map[int64]bool{}
	target := latest.Add(-10 * time.Minute)
	var nearest time.Duration
	foundTenMinuteSnapshot := false
	for _, snapshot := range snapshots {
		age := latest.Sub(snapshot.CreatedAt)
		if age >= 3*time.Minute && age <= 10*time.Minute+recentInterval {
			represented[snapshot.CreatedAt.UnixNano()/int64(recentInterval)] = true
		}
		distance := snapshot.CreatedAt.Sub(target)
		if distance < 0 {
			distance = -distance
		}
		if !foundTenMinuteSnapshot || distance < nearest {
			nearest, foundTenMinuteSnapshot = distance, true
		}
	}
	if !foundTenMinuteSnapshot || nearest > recentInterval {
		t.Fatalf("nearest retained snapshot is %v from the ten-minute mark", nearest)
	}
	firstBucket := latest.Add(-10*time.Minute).UnixNano()/int64(recentInterval) + 1
	lastBucket := latest.Add(-3*time.Minute).UnixNano() / int64(recentInterval)
	for bucket := firstBucket; bucket < lastBucket; bucket++ {
		if !represented[bucket] {
			t.Fatalf("missing 30-second checkpoint for bucket %d in ten-minute history", bucket)
		}
	}
}

func TestStoreSkipsExpiredHistoryAndHandlesEmptyText(t *testing.T) {
	store := New(t.TempDir())
	path := filepath.Join(t.TempDir(), "source.go")
	now := time.Now().UTC()
	if _, _, err := store.Add(path, "expired", now.Add(-maxAge-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, added, err := store.Add(path, "", now); err != nil || !added {
		t.Fatalf("add empty snapshot: added=%v err=%v", added, err)
	}
	got, err := store.List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "" {
		t.Fatalf("snapshots = %+v", got)
	}
}

func TestStoreReaddsContentFromExpiredSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "source.go")
	store := New(dir)
	canonical, err := canonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	old := Snapshot{ID: "expired", CreatedAt: time.Now().Add(-maxAge - time.Second), Text: "same"}
	if err := writeHistory(store.filePath(canonical), &fileHistory{Path: canonical, Snapshots: []Snapshot{old}}); err != nil {
		t.Fatal(err)
	}
	got, added, err := store.Add(path, "same", time.Now())
	if err != nil || !added || !got.CreatedAt.After(old.CreatedAt) {
		t.Fatalf("re-add expired content: snapshot=%+v added=%v err=%v", got, added, err)
	}
	snapshots, err := store.List(path)
	if err != nil || len(snapshots) != 1 || snapshots[0].ID != got.ID {
		t.Fatalf("snapshots = %+v, err=%v", snapshots, err)
	}
}
