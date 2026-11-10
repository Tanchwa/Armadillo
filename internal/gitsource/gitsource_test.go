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

package gitsource

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestCheckURL(t *testing.T) {
	f := GitFetcher{AllowedHosts: []string{"github.com"}}
	for url, ok := range map[string]bool{
		"https://github.com/Tanchwa/app.git":       true,
		"https://GitHub.com/Tanchwa/app.git":       true,
		"http://github.com/Tanchwa/app.git":        false,
		"ssh://git@github.com/Tanchwa/app.git":     false,
		"file:///etc":                              false,
		"https://169.254.169.254/latest/meta-data": false,
		"https://github.com.evil.example/x.git":    false,
		"https://user:token@github.com/x/y.git":    false,
		"https://github.com@evil.example/x.git":    false,
		"/var/lib/repo":                            false,
	} {
		if err := f.CheckURL(url); (err == nil) != ok {
			t.Errorf("%s: allowed=%v, err=%v", url, err == nil, err)
		}
	}
}

func TestCheckPath(t *testing.T) {
	for p, ok := range map[string]bool{
		"apparmor/myapp.profile": true,
		"myapp.profile":          true,
		"":                       false,
		"/etc/shadow":            false,
		"../other":               false,
		"a/../../b":              false,
		"a//b":                   false,
		"./a":                    false,
	} {
		if err := CheckPath(p); (err == nil) != ok {
			t.Errorf("%q: allowed=%v", p, err == nil)
		}
	}
}

func commit(t *testing.T, repo *git.Repository, dir, file, content string) plumbing.Hash {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(file); err != nil {
		t.Fatal(err)
	}
	hash, err := wt.Commit("update "+file, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestCloneReadsFileAtBranchTip(t *testing.T) {
	// go-git serves local repositories through the git binary.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	commit(t, repo, dir, "apparmor/myapp.profile", "first\n")
	second := commit(t, repo, dir, "apparmor/myapp.profile", "second\n")

	f := GitFetcher{MaxBytes: 1024}
	content, revision, err := f.clone(ctx, Repo{URL: dir, Path: "apparmor/myapp.profile"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if content != "second\n" || revision != second.String() {
		t.Errorf("content=%q revision=%s, want the second commit %s", content, revision, second)
	}

	if _, _, err := f.clone(ctx, Repo{URL: dir, Path: "missing.profile"}, nil); err == nil {
		t.Error("a missing file was not an error")
	}
	if _, _, err := f.clone(ctx, Repo{URL: dir, Path: "../escape"}, nil); err == nil {
		t.Error("a path outside the repository was not an error")
	}
	small := GitFetcher{MaxBytes: 3}
	if _, _, err := small.clone(ctx, Repo{URL: dir, Path: "apparmor/myapp.profile"}, nil); err == nil ||
		!strings.Contains(err.Error(), "larger than") {
		t.Errorf("an oversized file was not rejected: %v", err)
	}
}
