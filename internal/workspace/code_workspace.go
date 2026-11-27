package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"see/internal/uri"
)

// Folder is a root folder in a multi-root workspace.
type Folder struct {
	Path string
	Name string
}

// Settings are the VS Code workspace settings See applies.
type Settings struct {
	FilesExclude               map[string]bool   `json:"files.exclude"`
	FilesExcludeWhen           map[string]string `json:"-"`
	FilesWatcherExclude        map[string]bool   `json:"files.watcherExclude"`
	FilesWatcherExcludeWhen    map[string]string `json:"-"`
	SearchExclude              map[string]bool   `json:"search.exclude"`
	SearchExcludeWhen          map[string]string `json:"-"`
	SearchUseIgnoreFiles       *bool             `json:"search.useIgnoreFiles"`
	SearchUseGlobalIgnoreFiles *bool             `json:"search.useGlobalIgnoreFiles"`
	SearchUseParentIgnoreFiles *bool             `json:"search.useParentIgnoreFiles"`
	ExplorerExcludeGitIgnore   bool              `json:"explorer.excludeGitIgnore"`
}

// UnmarshalJSON reads VS Code exclusion expressions. Their values are either
// booleans or sibling clauses such as {"when": "$(basename).ts"}.
func (s *Settings) UnmarshalJSON(data []byte) error {
	var raw struct {
		FilesExclude               json.RawMessage `json:"files.exclude"`
		FilesWatcherExclude        json.RawMessage `json:"files.watcherExclude"`
		SearchExclude              json.RawMessage `json:"search.exclude"`
		SearchUseIgnoreFiles       *bool           `json:"search.useIgnoreFiles"`
		SearchUseGlobalIgnoreFiles *bool           `json:"search.useGlobalIgnoreFiles"`
		SearchUseParentIgnoreFiles *bool           `json:"search.useParentIgnoreFiles"`
		ExplorerExcludeGitIgnore   bool            `json:"explorer.excludeGitIgnore"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.FilesExclude, s.FilesExcludeWhen = parseExcludeExpression(raw.FilesExclude)
	s.FilesWatcherExclude, s.FilesWatcherExcludeWhen = parseExcludeExpression(raw.FilesWatcherExclude)
	s.SearchExclude, s.SearchExcludeWhen = parseExcludeExpression(raw.SearchExclude)
	s.SearchUseIgnoreFiles = raw.SearchUseIgnoreFiles
	s.SearchUseGlobalIgnoreFiles = raw.SearchUseGlobalIgnoreFiles
	s.SearchUseParentIgnoreFiles = raw.SearchUseParentIgnoreFiles
	s.ExplorerExcludeGitIgnore = raw.ExplorerExcludeGitIgnore
	return nil
}

func parseExcludeExpression(data json.RawMessage) (map[string]bool, map[string]string) {
	if len(data) == 0 {
		return nil, nil
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(data, &values) != nil {
		return nil, nil
	}
	patterns := map[string]bool{}
	clauses := map[string]string{}
	for pattern, raw := range values {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err == nil {
			patterns[pattern] = enabled
			continue
		}
		var clause struct {
			When string `json:"when"`
		}
		if err := json.Unmarshal(raw, &clause); err == nil && clause.When != "" {
			clauses[pattern] = clause.When
		}
	}
	if len(patterns) == 0 {
		patterns = nil
	}
	if len(clauses) == 0 {
		clauses = nil
	}
	return patterns, clauses
}

// UseIgnoreFiles reports whether search follows ignore files; VS Code's default is true.
func (s Settings) UseIgnoreFiles() bool {
	return s.SearchUseIgnoreFiles == nil || *s.SearchUseIgnoreFiles
}

// UseGlobalIgnoreFiles and UseParentIgnoreFiles follow VS Code's defaults.
func (s Settings) UseGlobalIgnoreFiles() bool {
	return s.SearchUseGlobalIgnoreFiles == nil || *s.SearchUseGlobalIgnoreFiles
}

func (s Settings) UseParentIgnoreFiles() bool {
	return s.SearchUseParentIgnoreFiles == nil || *s.SearchUseParentIgnoreFiles
}

// CodeWorkspace is a VS Code .code-workspace file.
type CodeWorkspace struct {
	Folders  []Folder
	Settings Settings
}

// ReadCodeWorkspace reads a VS Code .code-workspace file. VS Code resolves
// relative members against the opened URI lexically, so parent symlinks keep
// their spelling here instead of being canonicalized away.
func ReadCodeWorkspace(path string) (*CodeWorkspace, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	return parseCodeWorkspace(data, absolute)
}

func workspaceFolderEntryPath(pathValue, uriValue string) (string, error) {
	p := pathValue
	if strings.TrimSpace(uriValue) != "" {
		local, err := uri.ToPath(strings.TrimSpace(uriValue))
		if err != nil {
			return "", fmt.Errorf("remote workspace folder %q is not supported by See", uriValue)
		}
		p = local
	}
	if strings.TrimSpace(p) == "" {
		return "", errors.New("workspace folder has no path")
	}
	return p, nil
}

func parseCodeWorkspace(data []byte, absolute string) (*CodeWorkspace, error) {
	data, err := cleanWorkspaceJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse workspace: %w", err)
	}
	var file struct {
		Folders []struct {
			Path string `json:"path"`
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"folders"`
		Settings Settings `json:"settings"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse workspace: %w", err)
	}
	if len(file.Folders) == 0 {
		return nil, errors.New("workspace has no folders")
	}
	base := filepath.Dir(absolute)
	folders := make([]Folder, 0, len(file.Folders))
	seen := map[string]bool{}
	for _, entry := range file.Folders {
		p, err := workspaceFolderEntryPath(entry.Path, entry.URI)
		if err != nil {
			return nil, err
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, filepath.FromSlash(p))
		}
		root, err := CanonicalRoot(p)
		if err != nil {
			// Keep a missing member readable so the user can remove its stale
			// reference instead of being locked out of the whole workspace.
			absolute, absErr := filepath.Abs(p)
			if absErr != nil {
				return nil, fmt.Errorf("workspace folder %q: %w", entry.Path, err)
			}
			root = filepath.Clean(absolute)
		}
		if seen[root] {
			continue
		}
		seen[root] = true
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = filepath.Base(root)
		}
		folders = append(folders, Folder{Path: root, Name: name})
	}
	if len(folders) == 0 {
		return nil, errors.New("workspace has no folders")
	}
	return &CodeWorkspace{Folders: folders, Settings: file.Settings}, nil
}

// AddCodeWorkspaceFolder updates the on-disk workspace using its latest contents.
func AddCodeWorkspaceFolder(path string, folder Folder) ([]Folder, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	parsed, err := parseCodeWorkspace(data, absolute)
	if err != nil {
		return nil, err
	}
	folders := parsed.Folders
	root, err := CanonicalRoot(folder.Path)
	if err != nil {
		return nil, err
	}
	for _, existing := range folders {
		if existing.Path == root {
			return folders, nil
		}
	}
	array, err := parseWorkspaceFolderArray(data, absolute)
	if err != nil {
		return nil, err
	}
	entryPath, err := filepath.Rel(filepath.Dir(absolute), root)
	if err != nil || filepath.IsAbs(entryPath) {
		entryPath = root
	}
	entry := struct {
		Path string `json:"path"`
		Name string `json:"name,omitempty"`
	}{Path: filepath.ToSlash(entryPath), Name: strings.TrimSpace(folder.Name)}
	if entry.Name == "" {
		entry.Name = filepath.Base(root)
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	updated, err := insertWorkspaceFolder(data, array, encoded)
	if err != nil {
		return nil, err
	}
	updatedFolders, err := parseCodeWorkspace(updated, absolute)
	if err != nil {
		return nil, err
	}
	if err := writeWorkspaceFile(absolute, data, updated); err != nil {
		return nil, err
	}
	return updatedFolders.Folders, nil
}

// RemoveCodeWorkspaceFolder removes only references to root from the file.
func RemoveCodeWorkspaceFolder(path, root string) ([]Folder, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	parsed, err := parseCodeWorkspace(data, absolute)
	if err != nil {
		return nil, err
	}
	folders := parsed.Folders
	target, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	target = filepath.Clean(target)
	array, err := parseWorkspaceFolderArray(data, absolute)
	if err != nil {
		return nil, err
	}
	remove := make([]bool, len(array.elements))
	removed := 0
	for i, entry := range array.elements {
		entryRoot := entry.path
		if !filepath.IsAbs(entryRoot) {
			entryRoot = filepath.Join(filepath.Dir(absolute), filepath.FromSlash(entryRoot))
		}
		canonical, canonicalErr := CanonicalRoot(entryRoot)
		if canonicalErr != nil {
			canonical = filepath.Clean(entryRoot)
		}
		if canonical == target {
			remove[i] = true
			removed++
		}
	}
	if removed == 0 {
		return folders, nil
	}
	if len(array.elements)-removed == 0 {
		return nil, errors.New("workspace must contain at least one folder")
	}
	updated := removeWorkspaceFolders(data, array, remove)
	updatedFolders, err := parseCodeWorkspace(updated, absolute)
	if err != nil {
		return nil, err
	}
	if err := writeWorkspaceFile(absolute, data, updated); err != nil {
		return nil, err
	}
	return updatedFolders.Folders, nil
}

type workspaceFolderElement struct {
	start, end int
	comma      int
	path, name string
}

type workspaceFolderArray struct {
	start, end int
	elements   []workspaceFolderElement
}

func parseWorkspaceFolderArray(data []byte, path string) (workspaceFolderArray, error) {
	i := 0
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		i = 3
	}
	i = skipWorkspaceTrivia(data, i)
	if i >= len(data) || data[i] != '{' {
		return workspaceFolderArray{}, errors.New("parse workspace: expected object")
	}
	for i++; ; {
		i = skipWorkspaceTrivia(data, i)
		if i >= len(data) || data[i] == '}' {
			break
		}
		keyEnd, err := scanWorkspaceString(data, i)
		if err != nil {
			return workspaceFolderArray{}, err
		}
		var key string
		if err := json.Unmarshal(data[i:keyEnd], &key); err != nil {
			return workspaceFolderArray{}, err
		}
		i = skipWorkspaceTrivia(data, keyEnd)
		if i >= len(data) || data[i] != ':' {
			return workspaceFolderArray{}, errors.New("parse workspace: expected colon")
		}
		valueStart := skipWorkspaceTrivia(data, i+1)
		valueEnd, err := scanWorkspaceValue(data, valueStart)
		if err != nil {
			return workspaceFolderArray{}, err
		}
		if key == "folders" {
			if data[valueStart] != '[' {
				return workspaceFolderArray{}, errors.New("parse workspace: folders must be an array")
			}
			return parseWorkspaceFolderElements(data, valueStart, valueEnd, path)
		}
		i = skipWorkspaceTrivia(data, valueEnd)
		if i < len(data) && data[i] == ',' {
			i++
			continue
		}
		if i >= len(data) || data[i] != '}' {
			return workspaceFolderArray{}, errors.New("parse workspace: expected comma or object end")
		}
		break
	}
	return workspaceFolderArray{}, errors.New("parse workspace: missing folders array")
}

func parseWorkspaceFolderElements(data []byte, start, end int, path string) (workspaceFolderArray, error) {
	array := workspaceFolderArray{start: start, end: end}
	for i := skipWorkspaceTrivia(data, start+1); i < end-1; {
		if data[i] == ']' {
			break
		}
		valueEnd, err := scanWorkspaceValue(data, i)
		if err != nil || valueEnd > end-1 {
			return workspaceFolderArray{}, errors.New("parse workspace: invalid folder entry")
		}
		clean, err := cleanWorkspaceJSON(data[i:valueEnd])
		if err != nil {
			return workspaceFolderArray{}, err
		}
		var entry struct {
			Path string `json:"path"`
			Name string `json:"name"`
			URI  string `json:"uri"`
		}
		if err := json.Unmarshal(clean, &entry); err != nil {
			return workspaceFolderArray{}, fmt.Errorf("parse workspace: invalid folder entry in %s", path)
		}
		entryPath, pathErr := workspaceFolderEntryPath(entry.Path, entry.URI)
		if pathErr != nil {
			return workspaceFolderArray{}, fmt.Errorf("parse workspace: invalid folder entry in %s", path)
		}
		next := skipWorkspaceTrivia(data, valueEnd)
		comma := -1
		if next < end-1 && data[next] == ',' {
			comma = next
			next++
		} else if next >= end || data[next] != ']' {
			return workspaceFolderArray{}, errors.New("parse workspace: expected comma or folders end")
		}
		array.elements = append(array.elements, workspaceFolderElement{start: i, end: valueEnd, comma: comma, path: entryPath, name: entry.Name})
		i = skipWorkspaceTrivia(data, next)
	}
	return array, nil
}

func skipWorkspaceTrivia(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '/':
			if i+1 >= len(data) {
				return i
			}
			switch data[i+1] {
			case '/':
				i += 2
				for i < len(data) && data[i] != '\n' && data[i] != '\r' {
					i++
				}
			case '*':
				end := bytes.Index(data[i+2:], []byte("*/"))
				if end < 0 {
					return len(data)
				}
				i += end + 4
			default:
				return i
			}
		default:
			return i
		}
	}
	return i
}

func scanWorkspaceString(data []byte, start int) (int, error) {
	if start >= len(data) || data[start] != '"' {
		return 0, errors.New("parse workspace: expected string")
	}
	for i := start + 1; i < len(data); i++ {
		if data[i] == '\\' {
			i++
			continue
		}
		if data[i] == '"' {
			return i + 1, nil
		}
	}
	return 0, errors.New("parse workspace: unterminated string")
}

func scanWorkspaceValue(data []byte, start int) (int, error) {
	if start >= len(data) {
		return 0, errors.New("parse workspace: missing value")
	}
	if data[start] == '"' {
		return scanWorkspaceString(data, start)
	}
	if data[start] != '{' && data[start] != '[' {
		for i := start; i < len(data); i++ {
			if data[i] == ',' || data[i] == '}' || data[i] == ']' || data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n' || data[i] == '/' {
				return i, nil
			}
		}
		return len(data), nil
	}
	stack := []byte{data[start]}
	for i := start + 1; i < len(data); i++ {
		switch data[i] {
		case '"':
			end, err := scanWorkspaceString(data, i)
			if err != nil {
				return 0, err
			}
			i = end - 1
		case '/':
			if i+1 < len(data) && data[i+1] == '/' {
				i += 2
				for i < len(data) && data[i] != '\n' && data[i] != '\r' {
					i++
				}
			} else if i+1 < len(data) && data[i+1] == '*' {
				end := bytes.Index(data[i+2:], []byte("*/"))
				if end < 0 {
					return 0, errors.New("parse workspace: unterminated comment")
				}
				i += end + 3
			}
		case '{', '[':
			stack = append(stack, data[i])
		case '}', ']':
			open := stack[len(stack)-1]
			if (open == '{' && data[i] != '}') || (open == '[' && data[i] != ']') {
				return 0, errors.New("parse workspace: mismatched delimiter")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, errors.New("parse workspace: unterminated value")
}

func insertWorkspaceFolder(data []byte, array workspaceFolderArray, encoded []byte) ([]byte, error) {
	newline := []byte("\n")
	if bytes.Contains(data, []byte("\r\n")) {
		newline = []byte("\r\n")
	}
	indent := workspaceArrayIndent(data, array)
	var at int
	var fragment []byte
	if len(array.elements) == 0 {
		at = array.end - 1
		closeIndent := lineIndent(data, at)
		if bytes.Contains(data[array.start:array.end], []byte("\n")) {
			fragment = append(append(append(append([]byte(nil), newline...), indent...), encoded...), newline...)
			fragment = append(fragment, closeIndent...)
		} else {
			fragment = append([]byte(nil), encoded...)
		}
	} else {
		last := array.elements[len(array.elements)-1]
		if last.comma >= 0 {
			at = last.comma
			at++
			fragment = append(append(append([]byte(nil), newline...), indent...), encoded...)
			fragment = append(fragment, ',')
		} else {
			at = last.end
			fragment = append([]byte{','}, newline...)
			fragment = append(fragment, indent...)
			fragment = append(fragment, encoded...)
		}
	}
	return spliceWorkspaceBytes(data, at, at, fragment), nil
}

func removeWorkspaceFolders(data []byte, array workspaceFolderArray, remove []bool) []byte {
	type span struct{ start, end int }
	var spans []span
	for i := 0; i < len(remove); {
		if !remove[i] {
			i++
			continue
		}
		start, end := i, i
		for end+1 < len(remove) && remove[end+1] {
			end++
		}
		for j := start; j <= end; j++ {
			entry := array.elements[j]
			spans = append(spans, span{entry.start, entry.end})
			if j < end && entry.comma >= 0 {
				spans = append(spans, span{entry.comma, entry.comma + 1})
			}
		}
		if end < len(remove)-1 {
			if comma := array.elements[end].comma; comma >= 0 {
				spans = append(spans, span{comma, comma + 1})
			}
		} else if start > 0 {
			if comma := array.elements[start-1].comma; comma >= 0 {
				spans = append(spans, span{comma, comma + 1})
			}
		}
		i = end + 1
	}
	slices.SortFunc(spans, func(a, b span) int { return b.start - a.start })
	for _, span := range spans {
		data = spliceWorkspaceBytes(data, span.start, span.end, nil)
	}
	return data
}

func workspaceArrayIndent(data []byte, array workspaceFolderArray) []byte {
	if len(array.elements) > 0 {
		return lineIndent(data, array.elements[0].start)
	}
	return []byte("  ")
}

func lineIndent(data []byte, at int) []byte {
	start := bytes.LastIndex(data[:at], []byte("\n")) + 1
	for i := start; i < at; i++ {
		if data[i] != ' ' && data[i] != '\t' {
			return nil
		}
	}
	return append([]byte(nil), data[start:at]...)
}

func spliceWorkspaceBytes(data []byte, start, end int, insert []byte) []byte {
	result := make([]byte, 0, len(data)-(end-start)+len(insert))
	result = append(result, data[:start]...)
	result = append(result, insert...)
	result = append(result, data[end:]...)
	return result
}

func writeWorkspaceFile(path string, expected, data []byte) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".code-workspace-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return errors.New("workspace file changed during membership update")
	}
	return os.Rename(tempPath, path)
}

func cleanWorkspaceJSON(data []byte) ([]byte, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	var out bytes.Buffer
	quoted, escaped := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if quoted {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			out.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(data) {
			switch data[i+1] {
			case '/':
				i += 2
				for i < len(data) && data[i] != '\n' && data[i] != '\r' {
					i++
				}
				if i < len(data) {
					out.WriteByte(data[i])
				}
				continue
			case '*':
				end := bytes.Index(data[i+2:], []byte("*/"))
				if end < 0 {
					return nil, errors.New("unterminated comment")
				}
				comment := data[i : i+2+end+2]
				for _, c := range comment {
					if c == '\n' {
						out.WriteByte('\n')
					}
				}
				i += 2 + end + 1
				continue
			}
		}
		if c == ',' {
			next, err := nextWorkspaceJSONToken(data, i+1)
			if err != nil {
				return nil, err
			}
			if next == '}' || next == ']' {
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.Bytes(), nil
}

func nextWorkspaceJSONToken(data []byte, i int) (byte, error) {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '/':
			if i+1 >= len(data) {
				return '/', nil
			}
			switch data[i+1] {
			case '/':
				i += 2
				for i < len(data) && data[i] != '\n' && data[i] != '\r' {
					i++
				}
			case '*':
				end := bytes.Index(data[i+2:], []byte("*/"))
				if end < 0 {
					return 0, errors.New("unterminated comment")
				}
				i += 2 + end + 2
			default:
				return '/', nil
			}
		default:
			return data[i], nil
		}
	}
	return 0, nil
}

// WriteNewCodeWorkspace creates a workspace file containing folders. It
// refuses to overwrite an existing file; editing one is AddCodeWorkspaceFolder's work.
func WriteNewCodeWorkspace(path string, folders []Folder) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return errors.New("workspace has no folders")
	}
	data := []byte("{\n  \"folders\": [")
	for i, folder := range folders {
		root, err := CanonicalRoot(folder.Path)
		if err != nil {
			return err
		}
		entryPath, err := filepath.Rel(filepath.Dir(absolute), root)
		if err != nil || filepath.IsAbs(entryPath) {
			entryPath = root
		}
		name := strings.TrimSpace(folder.Name)
		if name == filepath.Base(root) {
			name = ""
		}
		entry := struct {
			Path string `json:"path"`
			Name string `json:"name,omitempty"`
		}{Path: filepath.ToSlash(entryPath), Name: name}
		encoded, err := json.MarshalIndent(entry, "    ", "  ")
		if err != nil {
			return err
		}
		if i > 0 {
			data = append(data, ',')
		}
		data = append(data, '\n', ' ', ' ', ' ', ' ')
		data = append(data, encoded...)
	}
	data = append(data, []byte("\n  ],\n  \"settings\": {}\n}\n")...)
	f, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	return errors.Join(writeErr, closeErr)
}
