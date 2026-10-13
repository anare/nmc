package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrNotARepo = errors.New("not a git repository")

type Commit struct {
	Hash, Short, Author, Subject string
	When                         time.Time
}

type Branch struct {
	Name string
	When time.Time
}

type Status struct {
	Staged, Modified, Untracked []string
}

func Root(ctx context.Context, dir string) (string, error) {
	output, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
			return "", ErrNotARepo
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func Log(ctx context.Context, dir, name string, limit int) ([]Commit, error) {
	root, err := Root(ctx, dir)
	if err != nil {
		return nil, err
	}
	args := []string{"log", "--format=%H%x00%h%x00%an%x00%at%x00%s", "-n", strconv.Itoa(maxInt(1, limit))}
	if name != "" {
		relative, err := relativePath(root, name)
		if err != nil {
			return nil, err
		}
		if info, statErr := os.Stat(filepath.Join(root, relative)); statErr == nil && info.Mode().IsRegular() {
			args = append(args, "--follow")
		}
		args = append(args, "--")
		args = append(args, relative)
	}
	output, err := run(ctx, root, args...)
	if err != nil {
		if noCommits(ctx, root) {
			return []Commit{}, nil
		}
		return nil, err
	}
	var commits []Commit
	for _, record := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.SplitN(record, "\x00", 5)
		if len(fields) != 5 {
			continue
		}
		seconds, _ := strconv.ParseInt(fields[3], 10, 64)
		commits = append(commits, Commit{Hash: fields[0], Short: fields[1], Author: fields[2], When: time.Unix(seconds, 0), Subject: fields[4]})
	}
	return commits, nil
}

func Show(ctx context.Context, dir, hash, name string) ([]byte, error) {
	root, err := Root(ctx, dir)
	if err != nil {
		return nil, err
	}
	args := []string{"show", "--format=", "--no-ext-diff", hash}
	if name != "" {
		relative, err := relativePath(root, name)
		if err != nil {
			return nil, err
		}
		args = append(args, "--", relative)
	}
	return run(ctx, root, args...)
}

func Diff(ctx context.Context, dir, name string) ([]byte, error) {
	root, err := Root(ctx, dir)
	if err != nil {
		return nil, err
	}
	args := []string{"diff", "--no-ext-diff"}
	if name != "" {
		relative, err := relativePath(root, name)
		if err != nil {
			return nil, err
		}
		args = append(args, "--", relative)
	}
	return run(ctx, root, args...)
}

func Describe(ctx context.Context, dir string) string {
	out, err := run(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	out, err = run(ctx, dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return "detached at " + strings.TrimSpace(string(out))
}

func CurrentStatus(ctx context.Context, dir string) (Status, error) {
	output, err := run(ctx, dir, "status", "--porcelain=v1", "-z")
	if err != nil {
		return Status{}, err
	}
	var result Status
	records := bytes.Split(output, []byte{0})
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 {
			continue
		}
		x, y := record[0], record[1]
		name := string(record[3:])
		switch {
		case x == '?' && y == '?':
			result.Untracked = append(result.Untracked, name)
		default:
			if x != ' ' && x != '?' {
				result.Staged = append(result.Staged, name)
			}
			if y != ' ' && y != '?' {
				result.Modified = append(result.Modified, name)
			}
			if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
				index++
			}
		}
	}
	return result, nil
}

func CommitChanges(ctx context.Context, dir, message string, stageAll bool) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("commit message is empty")
	}
	if stageAll {
		if _, err := runDetail(ctx, dir, "add", "-A"); err != nil {
			return "", err
		}
	}
	if _, err := runDetail(ctx, dir, "commit", "-m", message); err != nil {
		return "", err
	}
	output, err := run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func Branches(ctx context.Context, dir string) ([]Branch, error) {
	output, err := run(ctx, dir, "for-each-ref", "--format=%(refname:short)%00%(committerdate:unix)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	var branches []Branch
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.SplitN(line, "\x00", 2)
		if len(fields) != 2 || fields[0] == "" {
			continue
		}
		seconds, _ := strconv.ParseInt(fields[1], 10, 64)
		branches = append(branches, Branch{Name: fields[0], When: time.Unix(seconds, 0)})
	}
	return branches, nil
}

func Switch(ctx context.Context, dir, name string) error {
	_, err := runDetail(ctx, dir, "switch", name)
	return err
}

func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runCommand(ctx, dir, args...)
}

func runDetail(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return stdout.Bytes(), errors.New(detail)
		}
		return stdout.Bytes(), err
	}
	if strings.Contains(strings.ToLower(stdout.String()), "nothing to commit") {
		return stdout.Bytes(), errors.New(strings.TrimSpace(stdout.String()))
	}
	return stdout.Bytes(), nil
}

func runCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return output, errors.New(detail)
		}
		return output, err
	}
	return output, nil
}

func relativePath(root, name string) (string, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(root, name)
	}
	relative, err := filepath.Rel(root, name)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside repository", name)
	}
	return filepath.ToSlash(relative), nil
}

func noCommits(ctx context.Context, root string) bool {
	_, err := run(ctx, root, "rev-parse", "--verify", "HEAD")
	return err != nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
