/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package gitsource reads a profile from the application's git repository.
// Git is the source of truth for profiles; the manager is the only component
// that talks to it.
package gitsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Repo says where a profile lives and how to authenticate.
type Repo struct {
	URL      string
	Username string
	Password string
	// Branch is the branch to read. Empty means the repository's default branch.
	Branch string
	// Path is the profile file, relative to the repository root.
	Path string
}

// Fetcher reads a profile from a repository.
type Fetcher interface {
	// Fetch returns the file's content and the commit it was read from.
	Fetch(ctx context.Context, repo Repo) (content, revision string, err error)
}

// GitFetcher fetches over HTTPS from an allowlist of hosts.
type GitFetcher struct {
	// AllowedHosts are the hosts a repository URL may point at. The URL comes
	// from a Secret that any user of a namespace can write, so without this
	// list the manager could be pointed at any address it can reach.
	AllowedHosts []string
	// MaxBytes is the largest file that is read.
	MaxBytes int64
}

// CheckURL reports whether the manager may fetch from the URL.
func (f GitFetcher) CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("repository URL must use https, not %q", u.Scheme)
	}
	if u.User != nil {
		return errors.New("repository URL must not contain credentials; put them in the Secret's username and password keys")
	}
	if !slices.Contains(f.AllowedHosts, strings.ToLower(u.Hostname())) {
		return fmt.Errorf("host %q is not in the manager's --git-allowed-hosts", u.Hostname())
	}
	return nil
}

// CheckPath reports whether the path names a file inside the repository.
func CheckPath(p string) error {
	if p == "" || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("path %q must be a clean path relative to the repository root", p)
	}
	return nil
}

// Fetch implements Fetcher.
func (f GitFetcher) Fetch(ctx context.Context, repo Repo) (string, string, error) {
	if err := f.CheckURL(repo.URL); err != nil {
		return "", "", err
	}
	var auth transport.AuthMethod
	if repo.Password != "" {
		username := repo.Username
		if username == "" {
			// Forges ignore the username when the password is a token, but reject an empty one.
			username = "git"
		}
		auth = &githttp.BasicAuth{Username: username, Password: repo.Password}
	}
	return f.clone(ctx, repo, auth)
}

// clone reads repo.Path at the tip of the branch. Everything stays in memory:
// nothing from the repository touches the manager's filesystem, and hooks and
// filters in the repository are never run.
func (f GitFetcher) clone(ctx context.Context, repo Repo, auth transport.AuthMethod) (string, string, error) {
	if err := CheckPath(repo.Path); err != nil {
		return "", "", err
	}
	opts := &git.CloneOptions{
		URL:          repo.URL,
		Auth:         auth,
		Depth:        1,
		SingleBranch: true,
		Tags:         git.NoTags,
	}
	if repo.Branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(repo.Branch)
	}
	fs := memfs.New()
	r, err := git.CloneContext(ctx, memory.NewStorage(), fs, opts)
	if err != nil {
		return "", "", fmt.Errorf("cloning repository: %w", err)
	}
	head, err := r.Head()
	if err != nil {
		return "", "", fmt.Errorf("reading HEAD: %w", err)
	}
	file, err := fs.Open(repo.Path)
	if err != nil {
		return "", "", fmt.Errorf("opening %s at %s: %w", repo.Path, head.Hash(), err)
	}
	defer file.Close() //nolint:errcheck
	content, err := io.ReadAll(io.LimitReader(file, f.MaxBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("reading %s: %w", repo.Path, err)
	}
	if int64(len(content)) > f.MaxBytes {
		return "", "", fmt.Errorf("%s is larger than %d bytes", repo.Path, f.MaxBytes)
	}
	return string(content), head.Hash().String(), nil
}
