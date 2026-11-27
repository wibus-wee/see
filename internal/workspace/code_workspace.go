package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Folder is a root folder in a multi-root workspace.
type Folder struct {
	Path string
	Name string
}

// ReadCodeWorkspace reads the folders from a VS Code .code-workspace file.
// Workspace settings are intentionally not applied by See.
func ReadCodeWorkspace(path string) ([]Folder, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	data, err = cleanWorkspaceJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse workspace: %w", err)
	}
	var file struct {
		Folders []struct {
			Path string `json:"path"`
			Name string `json:"name"`
		} `json:"folders"`
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
		if strings.TrimSpace(entry.Path) == "" {
			return nil, errors.New("workspace folder has no path")
		}
		p := entry.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, filepath.FromSlash(p))
		}
		root, err := CanonicalRoot(p)
		if err != nil {
			return nil, fmt.Errorf("workspace folder %q: %w", entry.Path, err)
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
	return folders, nil
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
