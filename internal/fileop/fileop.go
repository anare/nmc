package fileop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/anare/nmc/internal/vfs"
)

type Pair struct {
	Src string
	Dst string
}

type Policy int

const (
	Fail Policy = iota
	Overwrite
	Skip
)

type Operation struct {
	Op     string
	Pairs  []Pair
	Paths  []string
	Policy Policy
	SrcFS  vfs.FS
	DstFS  vfs.FS
}

type Progress struct {
	Op, File            string
	FileDone, FileTotal int64
	Done, Total         int64
	Items, ItemTotal    int
	Started             time.Time
}

func (p Progress) Percent() int {
	if p.Total > 0 {
		if p.Done <= 0 {
			return 0
		}
		if p.Done >= p.Total {
			return 100
		}
		return int(float64(p.Done) / float64(p.Total) * 100)
	}
	if p.ItemTotal > 0 {
		if p.Items <= 0 {
			return 0
		}
		if p.Items >= p.ItemTotal {
			return 100
		}
		return int(float64(p.Items) / float64(p.ItemTotal) * 100)
	}
	return 0
}

type Result struct {
	Items     int
	Failures  []error
	Cancelled bool
}

func (r Result) Err() error { return errors.Join(r.Failures...) }

func Plan(sources []string, destination string) ([]Pair, error) {
	return PlanFS(context.Background(), nil, sources, nil, destination)
}

func PlanFS(ctx context.Context, srcFS vfs.FS, sources []string, dstFS vfs.FS, destination string) ([]Pair, error) {
	if len(sources) == 0 {
		return nil, errors.New("no source files")
	}
	destinationIsDir := false
	if dstFS == nil {
		info, err := os.Stat(destination)
		if err == nil {
			destinationIsDir = info.IsDir()
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else {
		entry, err := dstFS.Stat(ctx, destination)
		if err == nil {
			destinationIsDir = entry.IsDir
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if !destinationIsDir && len(sources) != 1 {
		return nil, errors.New("destination is not a directory")
	}
	pairs := make([]Pair, 0, len(sources))
	for _, src := range sources {
		dst := destination
		if destinationIsDir {
			dst = join(dstFS, destination, base(srcFS, src))
		}
		if sameFS(srcFS, dstFS) {
			if samePath(src, dst, srcFS) {
				return nil, errors.New("source and destination are identical")
			}
			if srcEntry, err := stat(ctx, srcFS, src); err != nil {
				return nil, err
			} else if srcEntry.IsDir && pathWithin(src, dst, isLocal(srcFS)) {
				return nil, errors.New("destination is inside source")
			}
		}
		pairs = append(pairs, Pair{Src: src, Dst: dst})
	}
	return pairs, nil
}

func ConflictsFS(ctx context.Context, srcFS vfs.FS, pairs []Pair, dstFS vfs.FS) ([]string, error) {
	var conflicts []string
	for _, pair := range pairs {
		_, err := stat(ctx, dstFS, pair.Dst)
		if err == nil {
			conflicts = append(conflicts, pair.Dst)
		} else if !errors.Is(err, os.ErrNotExist) {
			return conflicts, err
		}
	}
	return conflicts, nil
}

func Run(ctx context.Context, op Operation, report func(Progress)) Result {
	result := Result{}
	progress := Progress{Op: op.Op, Started: time.Now()}
	if op.Op == "Delete" {
		progress.ItemTotal = len(op.Paths)
	} else {
		progress.ItemTotal = len(op.Pairs)
		for _, pair := range op.Pairs {
			if n, _, err := measure(ctx, op.SrcFS, pair.Src); err == nil {
				progress.Total += n
			}
		}
	}
	send := func() {
		if report != nil {
			report(progress)
		}
	}
	send()

	if op.Op == "Delete" {
		for _, name := range op.Paths {
			if err := ctx.Err(); err != nil {
				result.Cancelled = true
				result.Failures = append(result.Failures, err)
				break
			}
			progress.File = name
			err := remove(ctx, op.SrcFS, name)
			if err != nil {
				result.Failures = append(result.Failures, fmt.Errorf("%s: %w", name, err))
			} else {
				result.Items++
			}
			progress.Items++
			send()
		}
		return result
	}
	if op.Op != "Copy" && op.Op != "Move" {
		result.Failures = append(result.Failures, fmt.Errorf("unsupported operation %q", op.Op))
		return result
	}
	for _, pair := range op.Pairs {
		if err := ctx.Err(); err != nil {
			result.Cancelled = true
			result.Failures = append(result.Failures, err)
			break
		}
		progress.File = pair.Src
		progress.FileDone = 0
		fileSize, _, _ := measure(ctx, op.SrcFS, pair.Src)
		progress.FileTotal = fileSize
		_, destinationErr := stat(ctx, op.DstFS, pair.Dst)
		if destinationErr == nil {
			switch op.Policy {
			case Skip:
				progress.Done += fileSize
				progress.FileDone = fileSize
				progress.Items++
				send()
				continue
			case Overwrite:
				if err := remove(ctx, op.DstFS, pair.Dst); err != nil {
					result.Failures = append(result.Failures, fmt.Errorf("%s: %w", pair.Dst, err))
					progress.Items++
					send()
					continue
				}
			default:
				result.Failures = append(result.Failures, fmt.Errorf("destination exists: %s", pair.Dst))
				progress.Items++
				send()
				continue
			}
		} else if !errors.Is(destinationErr, os.ErrNotExist) {
			result.Failures = append(result.Failures, fmt.Errorf("%s: %w", pair.Dst, destinationErr))
			progress.Items++
			send()
			continue
		}
		var err error
		if op.Op == "Move" && sameFS(op.SrcFS, op.DstFS) {
			err = rename(ctx, op.SrcFS, pair.Src, pair.Dst)
			if isCrossDeviceRenameError(err) {
				err = copyPath(ctx, op.SrcFS, pair.Src, op.DstFS, pair.Dst, &progress, send)
				if err == nil {
					err = remove(ctx, op.SrcFS, pair.Src)
				}
			}
		} else if op.Op == "Move" {
			err = copyPath(ctx, op.SrcFS, pair.Src, op.DstFS, pair.Dst, &progress, send)
			if err == nil {
				err = remove(ctx, op.SrcFS, pair.Src)
			}
		} else {
			err = copyPath(ctx, op.SrcFS, pair.Src, op.DstFS, pair.Dst, &progress, send)
		}
		if err != nil {
			result.Failures = append(result.Failures, fmt.Errorf("%s: %w", pair.Src, err))
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				result.Cancelled = true
			}
		} else {
			result.Items++
			if op.Op == "Move" && sameFS(op.SrcFS, op.DstFS) {
				progress.Done += fileSize
				progress.FileDone = fileSize
			}
		}
		progress.Items++
		send()
	}
	return result
}

func copyPath(ctx context.Context, srcFS vfs.FS, src string, dstFS vfs.FS, dst string, progress *Progress, report func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := stat(ctx, srcFS, src)
	if err != nil {
		return err
	}
	if entry.IsLink {
		target, err := readlink(ctx, srcFS, src)
		if err != nil {
			return err
		}
		if err := symlink(ctx, dstFS, target, dst); err != nil {
			return err
		}
		return nil
	}
	if entry.IsDir {
		if err := mkdir(ctx, dstFS, dst); err != nil {
			return err
		}
		children, err := list(ctx, srcFS, src)
		if err != nil {
			_ = remove(ctx, dstFS, dst)
			return err
		}
		for _, child := range children {
			if child.IsUp || child.Name == "." {
				continue
			}
			if err := copyPath(ctx, srcFS, join(srcFS, src, child.Name), dstFS, join(dstFS, dst, child.Name), progress, report); err != nil {
				_ = remove(context.Background(), dstFS, dst)
				return err
			}
		}
		if err := preserveLocalMetadata(src, dst, entry, srcFS, dstFS); err != nil {
			_ = remove(context.Background(), dstFS, dst)
			return err
		}
		return nil
	}
	reader, err := open(ctx, srcFS, src)
	if err != nil {
		return err
	}
	writer, err := create(ctx, dstFS, dst, entry.Mode.Perm())
	if err != nil {
		_ = reader.Close()
		return err
	}
	buf := make([]byte, 32*1024)
	_, copyErr := io.CopyBuffer(&progressWriter{ctx: ctx, dst: writer, progress: progress, report: report}, reader, buf)
	readCloseErr := reader.Close()
	writeCloseErr := writer.Close()
	if copyErr != nil {
		_ = remove(context.Background(), dstFS, dst)
		return copyErr
	}
	if readCloseErr != nil {
		_ = remove(context.Background(), dstFS, dst)
		return readCloseErr
	}
	if writeCloseErr != nil {
		_ = remove(context.Background(), dstFS, dst)
		return writeCloseErr
	}
	if err := preserveLocalMetadata(src, dst, entry, srcFS, dstFS); err != nil {
		_ = remove(context.Background(), dstFS, dst)
		return err
	}
	return nil
}

func preserveLocalMetadata(src, dst string, entry vfs.Entry, srcFS, dstFS vfs.FS) error {
	if srcFS != nil || dstFS != nil || entry.IsLink {
		return nil
	}
	if err := os.Chmod(dst, entry.Mode.Perm()); err != nil {
		return err
	}
	return os.Chtimes(dst, entry.ModTime, entry.ModTime)
}

type progressWriter struct {
	ctx      context.Context
	dst      io.Writer
	progress *Progress
	report   func()
}

func (w *progressWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.dst.Write(data)
	w.progress.FileDone += int64(n)
	w.progress.Done += int64(n)
	if w.report != nil {
		w.report()
	}
	return n, err
}

func measure(ctx context.Context, fs vfs.FS, name string) (int64, int, error) {
	entry, err := stat(ctx, fs, name)
	if err != nil {
		return 0, 0, err
	}
	if entry.IsLink {
		return 0, 1, nil
	}
	if !entry.IsDir {
		if entry.SizeUnknown {
			return 0, 1, nil
		}
		return max(int64(0), entry.Size), 1, nil
	}
	children, err := list(ctx, fs, name)
	if err != nil {
		return 0, 0, err
	}
	var size int64
	items := 1
	for _, child := range children {
		if child.IsUp {
			continue
		}
		n, count, err := measure(ctx, fs, join(fs, name, child.Name))
		if err != nil {
			return size, items, err
		}
		size += n
		items += count
	}
	return size, items, nil
}

func sameFS(a, b vfs.FS) bool {
	if a == nil || b == nil {
		if a == nil && b == nil {
			return true
		}
		other := a
		if other == nil {
			other = b
		}
		return other.Name() == ""
	}
	if a.Name() == "" && b.Name() == "" {
		return true
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	return va.Type() == vb.Type() && va.Type().Comparable() && va.Interface() == vb.Interface()
}

func stat(ctx context.Context, fs vfs.FS, name string) (vfs.Entry, error) {
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return vfs.Entry{}, err
		}
		info, err := os.Lstat(name)
		if err != nil {
			return vfs.Entry{}, err
		}
		entry := vfs.Entry{Name: filepath.Base(name), IsDir: info.IsDir(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}
		if info.Mode()&os.ModeSymlink != 0 {
			entry.IsLink = true
			entry.Target, err = os.Readlink(name)
			if err != nil {
				return vfs.Entry{}, err
			}
			target, statErr := os.Stat(name)
			entry.Broken = statErr != nil
			if statErr == nil {
				entry.IsDir = target.IsDir()
			}
		}
		return entry, nil
	}
	return fs.Stat(ctx, name)
}

func list(ctx context.Context, fs vfs.FS, name string) ([]vfs.Entry, error) {
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items, err := os.ReadDir(name)
		if err != nil {
			return nil, err
		}
		entries := make([]vfs.Entry, 0, len(items))
		for _, item := range items {
			info, err := os.Lstat(filepath.Join(name, item.Name()))
			if err != nil {
				return nil, err
			}
			entry := vfs.Entry{Name: item.Name(), IsDir: info.IsDir(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}
			if info.Mode()&os.ModeSymlink != 0 {
				entry.IsLink = true
				entry.Target, err = os.Readlink(filepath.Join(name, item.Name()))
				if err != nil {
					return nil, err
				}
			}
			entries = append(entries, entry)
		}
		return entries, nil
	}
	return fs.List(ctx, name)
}

func open(ctx context.Context, fs vfs.FS, name string) (io.ReadCloser, error) {
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return os.Open(name)
	}
	return fs.Open(ctx, name)
}

func create(ctx context.Context, fs vfs.FS, name string, mode os.FileMode) (io.WriteCloser, error) {
	if mode == 0 {
		mode = 0o644
	}
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	}
	return fs.Create(ctx, name, mode)
}

func mkdir(ctx context.Context, fs vfs.FS, name string) error {
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return os.Mkdir(name, 0o755)
	}
	return fs.Mkdir(ctx, name)
}

func remove(ctx context.Context, fs vfs.FS, name string) error {
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return os.RemoveAll(name)
	}
	return fs.Remove(ctx, name)
}

func rename(ctx context.Context, fs vfs.FS, from, to string) error {
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return os.Rename(from, to)
	}
	return fs.Rename(ctx, from, to)
}

func symlink(ctx context.Context, fs vfs.FS, target, link string) error {
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return os.Symlink(target, link)
	}
	return fs.Symlink(ctx, target, link)
}

func readlink(ctx context.Context, fs vfs.FS, name string) (string, error) {
	if fs == nil {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return os.Readlink(name)
	}
	return fs.Readlink(ctx, name)
}

func join(fs vfs.FS, parts ...string) string {
	if fs != nil {
		return fs.Join(parts...)
	}
	return filepath.Join(parts...)
}

func base(fs vfs.FS, name string) string {
	if fs != nil {
		if fs.Name() != "" {
			return path.Base(name)
		}
	}
	return filepath.Base(name)
}

func pathsEqual(a, b string) bool {
	a, b = canonical(a), canonical(b)
	if runtimeOSIsWindows() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func pathWithin(base, candidate string, local bool) bool {
	if local {
		base, candidate = canonical(base), canonical(candidate)
		relative, err := filepath.Rel(base, candidate)
		if err != nil {
			return false
		}
		return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	base, candidate = path.Clean(base), path.Clean(candidate)
	return candidate == base || strings.HasPrefix(candidate, strings.TrimSuffix(base, "/")+"/")
}

func samePath(a, b string, fs vfs.FS) bool {
	if isLocal(fs) {
		return pathsEqual(a, b)
	}
	return path.Clean(a) == path.Clean(b)
}

func isLocal(fs vfs.FS) bool { return fs == nil || fs.Name() == "" }

func canonical(name string) string {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return filepath.Clean(name)
	}
	absolute = filepath.Clean(absolute)
	current := absolute
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute
		}
		tail = append(tail, filepath.Base(current))
		current = parent
	}
}

func runtimeOSIsWindows() bool { return os.PathSeparator == '\\' }

func isCrossDeviceRenameError(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && errors.Is(linkErr.Err, syscall.EXDEV) {
		return true
	}
	if err != nil && os.PathSeparator == '\\' {
		message := strings.ToLower(err.Error())
		return strings.Contains(message, "not same device") || strings.Contains(message, "cross-device")
	}
	return false
}
