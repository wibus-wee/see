package git

import (
	"context"
	"strings"
)

type Ref struct {
	Name     string
	FullName string
}

type RemoteRefs struct {
	Name     string
	Branches []Ref
}

type RepositoryRefs struct {
	Current        string
	Branches, Tags []Ref
	Remotes        []RemoteRefs
}

func parseRepositoryRefs(refs, current string, remotes []string) RepositoryRefs {
	repo := RepositoryRefs{Current: current}
	for _, name := range remotes {
		repo.Remotes = append(repo.Remotes, RemoteRefs{Name: name})
	}
	for line := range strings.SplitSeq(refs, "\n") {
		full, rest, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		if name, ok := strings.CutPrefix(full, "refs/heads/"); ok {
			repo.Branches = append(repo.Branches, Ref{Name: name, FullName: full})
		} else if name, ok := strings.CutPrefix(full, "refs/tags/"); ok {
			repo.Tags = append(repo.Tags, Ref{Name: name, FullName: full})
		} else if name, ok := strings.CutPrefix(full, "refs/remotes/"); ok {
			_, symref, _ := strings.Cut(rest, "\x00")
			if symref != "" {
				continue
			}
			i := -1
			for j, r := range repo.Remotes {
				if strings.HasPrefix(name, r.Name+"/") && (i < 0 || len(r.Name) > len(repo.Remotes[i].Name)) {
					i = j
				}
			}
			if i >= 0 {
				r := &repo.Remotes[i]
				r.Branches = append(r.Branches, Ref{Name: name[len(r.Name)+1:], FullName: full})
			}
		}
	}
	return repo
}

func CreateTag(ctx context.Context, root, name, message, commit string) error {
	hash, err := resolveCommit(ctx, root, commit)
	if err != nil {
		return err
	}
	args := []string{"tag", "--no-sign"}
	if strings.TrimSpace(message) != "" {
		args = append(args, "--annotate", "--message", message)
	}
	_, err = Command(ctx, root, append(args, "--", name, hash)...)
	return err
}

func CherryPick(ctx context.Context, root, commit string) error {
	hash, err := resolveCommit(ctx, root, commit)
	if err != nil {
		return err
	}
	_, err = Command(ctx, root, "cherry-pick", hash)
	return err
}
