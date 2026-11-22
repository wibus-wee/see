package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// FileAtCommit follows the current path backwards through renames from HEAD.
func FileAtCommit(ctx context.Context, root, path, hash string) ([]byte, error) {
	path = filepath.ToSlash(path)
	out, err := Command(ctx, root, "log", "--follow", "--format=%x00%H", "--name-status", "-z", "HEAD", "--", path)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		token := strings.TrimSpace(parts[i])
		if strings.HasSuffix(token, hash) {
			return Command(ctx, root, "show", hash+":"+path)
		}
		if strings.HasPrefix(token, "R") && i+2 < len(parts) {
			path = parts[i+1]
			i += 2
		}
	}
	return nil, fmt.Errorf("revision %s is not in file history", hash)
}
