package vfs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type FS interface {
	Name() string
	List(ctx context.Context, dir string) ([]Entry, error)
	Stat(ctx context.Context, path string) (Entry, error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Create(ctx context.Context, path string, mode os.FileMode) (io.WriteCloser, error)
	Mkdir(ctx context.Context, path string) error
	Remove(ctx context.Context, path string) error
	Rename(ctx context.Context, from, to string) error
	Symlink(ctx context.Context, target, link string) error
	Readlink(ctx context.Context, path string) (string, error)
	Link(ctx context.Context, target, link string) error
	Join(elem ...string) string
	Dir(path string) string
	IsAbs(path string) bool
}

type Entry struct {
	Name        string
	IsDir       bool
	IsLink      bool
	Broken      bool
	IsUp        bool
	Size        int64
	ModTime     time.Time
	Mode        os.FileMode
	Target      string
	Links       int
	SizeUnknown bool
}

type Class string

const (
	ClassPlain   Class = "plain"
	ClassDir     Class = "directory"
	ClassExec    Class = "executable"
	ClassLink    Class = "symlink"
	ClassBroken  Class = "broken symlink"
	ClassSpecial Class = "special"
	ClassArchive Class = "archive"
	ClassImage   Class = "image"
	ClassMedia   Class = "media"
	ClassSource  Class = "source"
	ClassDoc     Class = "document"
	ClassData    Class = "data"
	ClassTemp    Class = "temporary"
)

var extClass = map[string]Class{
	"7z": ClassArchive, "bz2": ClassArchive, "gz": ClassArchive, "rar": ClassArchive, "tar": ClassArchive, "tgz": ClassArchive, "xz": ClassArchive, "zip": ClassArchive, "zst": ClassArchive,
	"bmp": ClassImage, "gif": ClassImage, "heic": ClassImage, "jpeg": ClassImage, "jpg": ClassImage, "png": ClassImage, "svg": ClassImage, "webp": ClassImage,
	"aac": ClassMedia, "flac": ClassMedia, "m4a": ClassMedia, "mkv": ClassMedia, "mp3": ClassMedia, "mp4": ClassMedia, "ogg": ClassMedia, "wav": ClassMedia, "webm": ClassMedia,
	"c": ClassSource, "cc": ClassSource, "cpp": ClassSource, "css": ClassSource, "go": ClassSource, "h": ClassSource, "html": ClassSource, "java": ClassSource, "js": ClassSource, "jsx": ClassSource, "py": ClassSource, "rs": ClassSource, "sh": ClassSource, "ts": ClassSource, "tsx": ClassSource,
	"1": ClassDoc, "md": ClassDoc, "pdf": ClassDoc, "rtf": ClassDoc, "txt": ClassDoc,
	"conf": ClassData, "csv": ClassData, "ini": ClassData, "json": ClassData, "toml": ClassData, "xml": ClassData, "yaml": ClassData, "yml": ClassData,
	"bak": ClassTemp, "log": ClassTemp, "old": ClassTemp, "orig": ClassTemp, "swp": ClassTemp, "tmp": ClassTemp,
}

var nameClass = map[string]Class{
	".gitignore": ClassData, ".env": ClassData, "makefile": ClassSource, "readme": ClassDoc, "go.mod": ClassData, "go.sum": ClassData,
}

func (e Entry) IsExec() bool {
	return !e.IsDir && !e.IsLink && e.Mode.IsRegular() && e.Mode.Perm()&0111 != 0
}

func (e Entry) IsSpecial() bool {
	return e.Mode&os.ModeType != 0 && !e.IsDir && !e.IsLink
}

func (e Entry) IsHardLink() bool {
	return e.Links > 1 && !e.IsDir && !e.IsLink
}

func (e Entry) Ext() string {
	ext := filepath.Ext(e.Name)
	if len(ext) <= 1 {
		return ""
	}
	return strings.ToLower(ext[1:])
}

func (e Entry) Indicator() string {
	switch {
	case e.IsDir:
		return "/"
	case e.IsLink:
		return "@"
	case e.IsExec():
		return "*"
	case e.IsSpecial():
		switch e.Mode & os.ModeType {
		case os.ModeDevice, os.ModeCharDevice:
			return "="
		case os.ModeNamedPipe:
			return "|"
		case os.ModeSocket:
			return "="
		}
	}
	return ""
}

func (e Entry) SizeString(width int) string {
	switch {
	case e.IsUp:
		return "UP--DIR"
	case e.IsDir:
		return "DIR"
	case e.Broken:
		return "!LINK"
	case e.IsSpecial():
		return "SPECIAL"
	case e.SizeUnknown:
		return "--"
	}
	if width < 1 {
		return ""
	}
	size := e.Size
	value := float64(size)
	unit := ""
	for _, u := range []string{"", "K", "M", "G", "T", "P"} {
		unit = u
		if value < 1024 || u == "P" {
			break
		}
		value /= 1024
	}
	result := ""
	if unit == "" {
		result = strconvFormatInt(size)
	} else if value < 10 {
		result = strconvFormatFloat(value, 1) + unit
	} else {
		result = strconvFormatFloat(value, 0) + unit
	}
	if len(result) > width {
		return strings.Repeat(">", width)
	}
	return strings.Repeat(" ", width-len(result)) + result
}

func (e Entry) Class() Class {
	switch {
	case e.IsUp, e.IsDir:
		return ClassDir
	case e.Broken:
		return ClassBroken
	case e.IsLink:
		return ClassLink
	case e.IsSpecial():
		return ClassSpecial
	case e.IsExec():
		return ClassExec
	}
	if strings.HasSuffix(e.Name, "~") {
		return ClassTemp
	}
	if class, ok := nameClass[strings.ToLower(e.Name)]; ok {
		return class
	}
	if class, ok := extClass[e.Ext()]; ok {
		return class
	}
	return ClassPlain
}

func strconvFormatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

func strconvFormatFloat(n float64, precision int) string {
	return strconv.FormatFloat(n, 'f', precision, 64)
}
