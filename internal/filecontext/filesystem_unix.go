//go:build darwin || linux

package filecontext

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const maxDirectoryEntries = 10_000

func slicesContainGit(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.EqualFold(part, ".git") {
			return true
		}
	}
	return false
}

// Walk from a pinned descriptor, never re-open a checked absolute pathname.
// O_NONBLOCK prevents a regular-file-to-FIFO swap from hanging before fstat.
func openWithin(root, rel string, directory bool) (*os.File, error) {
	if !filepath.IsAbs(root) {
		return nil, fail(CodePathNotAllowed)
	}
	if rel != "" {
		if _, ok := safeRelativePath(rel); !ok {
			return nil, fail(CodePathNotAllowed)
		}
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fail(CodeFileNotFound)
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fail(CodePathNotAllowed)
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(canonical), "/"), "/")
	if rel != "" {
		parts = append(parts, strings.Split(filepath.Clean(rel), string(filepath.Separator))...)
	}
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if index < len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := unix.Openat(fd, part, flags, 0)
		unix.Close(fd)
		if openErr != nil {
			if errors.Is(openErr, unix.ENOENT) {
				return nil, fail(CodeFileNotFound)
			}
			return nil, fail(CodePathNotAllowed)
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), "workspace-resource")
	info, err := f.Stat()
	if err != nil || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		f.Close()
		return nil, fail(CodePathNotAllowed)
	}
	return f, nil
}

func readBounded(ctx context.Context, root, rel string, limit int) ([]byte, os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	f, err := openWithin(root, rel, false)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return nil, nil, fail(CodeFileNotFound)
	}
	if before.Size() > int64(limit) {
		return nil, nil, fail(CodeFileTooLarge)
	}
	body, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, nil, fail(CodeFileNotFound)
	}
	if len(body) > limit {
		return nil, nil, fail(CodeFileTooLarge)
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, nil, fail(CodeFileChanged)
	}
	return body, after, nil
}

func readDirectory(ctx context.Context, root, rel string) ([]WorkspaceEntry, error) {
	f, err := openWithin(root, rel, true)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	children, err := f.ReadDir(maxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fail(CodeFileNotFound)
	}
	if len(children) > maxDirectoryEntries {
		return nil, fail(CodeOverloaded)
	}
	entries := make([]WorkspaceEntry, 0, len(children))
	for _, child := range children {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var stat unix.Stat_t
		if unix.Fstatat(int(f.Fd()), child.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW) != nil {
			return nil, fail(CodeDirectoryChanged)
		}
		kind, readable := WorkspaceEntryFile, true
		switch uint32(stat.Mode) & unix.S_IFMT {
		case unix.S_IFREG:
		case unix.S_IFDIR:
			kind = WorkspaceEntryDirectory
		case unix.S_IFLNK:
			kind, readable = WorkspaceEntrySymlink, false
		default:
			continue
		}
		path := child.Name()
		if rel != "" {
			path = rel + "/" + path
		}
		if strings.EqualFold(child.Name(), ".git") {
			readable = false
		}
		entries = append(entries, WorkspaceEntry{Name: child.Name(), Path: path, Kind: kind, SizeBytes: stat.Size, ModifiedAtMS: stat.Mtim.Sec*1000 + stat.Mtim.Nsec/1e6, Readable: readable})
	}
	return entries, nil
}
