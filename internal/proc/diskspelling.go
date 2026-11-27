package proc

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// foldKey is the key two spellings of one file share on a
// case-insensitive, normalization-insensitive file system: Unicode NFC
// and case-folded. It is a candidate relation only — whether two
// spellings really are one file is the file system's to say, which
// diskSpelling probes with os.SameFile rather than trusting this key.
func foldKey(name string) string {
	return strings.ToLower(string(norm.NFC.Bytes([]byte(name))))
}

// diskSpelling is p with the case and Unicode normalization the disk
// actually spells each existing component by. Components that do not
// exist keep their spelling, and once one does, nothing deeper can be
// probed. It exists for the trust boundary: filepath.EvalSymlinks
// resolves links but not the volume's own spelling, so a case variant
// of a workspace path on a case-insensitive file system names the same
// bytes while comparing lexically different, which is exactly how a
// path copied with the wrong case would evade the workspace-executable
// denial.
//
// NOTE(lane integration): Lane B implements the same primitive as
// workspace.DiskSpelling for document identity. The shapes are meant to
// be identical — per-component directory reads, a fold-equal candidate
// accepted only when os.SameFile proves the input spelling resolves to
// it — so when both lanes land, this copy should be deleted in favor of
// the workspace one rather than tuned further.
func diskSpelling(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	vol := filepath.VolumeName(abs)
	rest := abs[len(vol):]
	rooted := strings.HasPrefix(rest, string(filepath.Separator))
	cur := vol + string(filepath.Separator)
	var comps []string
	if rooted {
		comps = strings.Split(strings.TrimPrefix(rest, string(filepath.Separator)), string(filepath.Separator))
	} else {
		// A drive-relative Windows path: its meaning depends on the
		// drive's working directory, which probing cannot honor.
		return p
	}
	// On Windows the root of the volume needs no probing; UNC roots
	// cannot be listed with their own spelling either way.
	for i, comp := range comps {
		if comp == "" {
			continue
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			break
		}
		exact := false
		candidate := ""
		for _, e := range entries {
			if e.Name() == comp {
				exact = true
				break
			}
			if candidate == "" && foldKey(e.Name()) == foldKey(comp) {
				candidate = e.Name()
			}
		}
		spelling := comp
		if !exact && candidate != "" {
			// A fold-equal entry is the same file only when the file
			// system itself resolves the input spelling to it: on a
			// case-insensitive or normalization-insensitive file system
			// the input stats to the same inode, while on a sensitive one
			// it stats nowhere or somewhere else, and the input spelling
			// stands, as two spellings may be two files there.
			inputStat, err1 := os.Lstat(filepath.Join(cur, comp))
			candStat, err2 := os.Lstat(filepath.Join(cur, candidate))
			if err1 == nil && err2 == nil && os.SameFile(inputStat, candStat) {
				spelling = candidate
			}
		}
		comps[i] = spelling
		cur = filepath.Join(cur, spelling)
	}
	if len(comps) == 0 || len(comps) == 1 && comps[0] == "" {
		return filepath.Clean(vol + string(filepath.Separator))
	}
	return filepath.Join(vol+string(filepath.Separator), filepath.Join(comps...))
}

