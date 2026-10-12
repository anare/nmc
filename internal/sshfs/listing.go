package sshfs

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anare/nmc/internal/vfs"
)

func ParseGNUListing(output string) ([]vfs.Entry, error) {
	return parseStatListing(output, true)
}

func ParseBSDListing(output string) ([]vfs.Entry, error) {
	return parseStatListing(output, false)
}

func parseStatListing(output string, hexadecimalMode bool) ([]vfs.Entry, error) {
	var entries []vfs.Entry
	for lineNumber, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 7)
		if len(fields) != 7 || fields[0] != "L" {
			return nil, fmt.Errorf("invalid stat listing record on line %d", lineNumber+1)
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid size on line %d: %w", lineNumber+1, err)
		}
		seconds, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp on line %d: %w", lineNumber+1, err)
		}
		base := 8
		if hexadecimalMode {
			base = 16
		}
		rawMode := strings.TrimPrefix(strings.ToLower(fields[4]), "0x")
		mode, err := strconv.ParseUint(rawMode, base, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid mode on line %d: %w", lineNumber+1, err)
		}
		links, err := strconv.Atoi(fields[5])
		if err != nil {
			return nil, fmt.Errorf("invalid link count on line %d: %w", lineNumber+1, err)
		}
		entry := vfs.Entry{
			Name: fields[6], Size: size, ModTime: time.Unix(seconds, 0),
			Mode: unixFileMode(mode), Links: links,
		}
		setKind(&entry, fields[1], mode)
		entries = append(entries, entry)
	}
	return entries, nil
}

func ParseLSListing(output string) ([]vfs.Entry, error) {
	var entries []vfs.Entry
	for lineNumber, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 7 {
			return nil, fmt.Errorf("invalid ls listing record on line %d", lineNumber+1)
		}
		size, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid size on line %d: %w", lineNumber+1, err)
		}
		seconds, err := strconv.ParseInt(fields[5], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp on line %d: %w", lineNumber+1, err)
		}
		name := strings.TrimSpace(lineTail(line, 6))
		entry := vfs.Entry{
			Name: name, Size: size, ModTime: time.Unix(seconds, 0),
			Mode: parsePermissions(fields[0]),
		}
		entry.IsDir = entry.Mode.IsDir()
		entry.IsLink = entry.Mode&os.ModeSymlink != 0
		if links, parseErr := strconv.Atoi(fields[1]); parseErr == nil {
			entry.Links = links
		}
		if entry.IsLink {
			if link, target, ok := strings.Cut(name, " -> "); ok {
				entry.Name, entry.Target = link, target
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func ParseSymlinkRecords(output string, entries []vfs.Entry) error {
	byName := make(map[string]int, len(entries))
	for index := range entries {
		byName[entries[index].Name] = index
	}
	for lineNumber, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || fields[0] != "R" {
			return fmt.Errorf("invalid symlink listing record on line %d", lineNumber+1)
		}
		if index, ok := byName[fields[1]]; ok {
			entries[index].IsLink = true
			entries[index].Target = fields[2]
		}
	}
	return nil
}

func lineTail(line string, fields int) string {
	index := 0
	for index < len(line) {
		for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
			index++
		}
		if index == len(line) {
			return ""
		}
		fields--
		for index < len(line) && line[index] != ' ' && line[index] != '\t' {
			index++
		}
		if fields == 0 {
			for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
				index++
			}
			return line[index:]
		}
	}
	return ""
}

func setKind(entry *vfs.Entry, kind string, rawMode uint64) {
	switch strings.ToLower(kind) {
	case "directory":
		entry.IsDir = true
		entry.Mode |= os.ModeDir
	case "symbolic link":
		entry.IsLink = true
		entry.Mode |= os.ModeSymlink
	case "regular file":
	default:
		if strings.Contains(kind, "socket") {
			entry.Mode |= os.ModeSocket
		} else if strings.Contains(kind, "fifo") || strings.Contains(kind, "pipe") {
			entry.Mode |= os.ModeNamedPipe
		} else if strings.Contains(kind, "character") {
			entry.Mode |= os.ModeDevice | os.ModeCharDevice
		} else if strings.Contains(kind, "block") {
			entry.Mode |= os.ModeDevice
		}
	}
	switch rawMode & 0170000 {
	case 0040000:
		entry.IsDir = true
		entry.Mode |= os.ModeDir
	case 0120000:
		entry.IsLink = true
		entry.Mode |= os.ModeSymlink
	case 0140000:
		entry.Mode |= os.ModeSocket
	case 0010000:
		entry.Mode |= os.ModeNamedPipe
	case 0020000:
		entry.Mode |= os.ModeDevice | os.ModeCharDevice
	case 0060000:
		entry.Mode |= os.ModeDevice
	}
}

func unixFileMode(mode uint64) os.FileMode {
	result := os.FileMode(mode & 0o777)
	if mode&0o4000 != 0 {
		result |= os.ModeSetuid
	}
	if mode&0o2000 != 0 {
		result |= os.ModeSetgid
	}
	if mode&0o1000 != 0 {
		result |= os.ModeSticky
	}
	return result
}

func parsePermissions(value string) os.FileMode {
	if len(value) < 10 {
		return 0
	}
	var mode os.FileMode
	switch value[0] {
	case 'd':
		mode |= os.ModeDir
	case 'l':
		mode |= os.ModeSymlink
	case 'p':
		mode |= os.ModeNamedPipe
	case 's':
		mode |= os.ModeSocket
	case 'b':
		mode |= os.ModeDevice
	case 'c':
		mode |= os.ModeDevice | os.ModeCharDevice
	}
	for index, bit := range []os.FileMode{0o400, 0o200, 0o100, 0o040, 0o020, 0o010, 0o004, 0o002, 0o001} {
		if value[index+1] != '-' {
			mode |= bit
		}
	}
	if value[3] == 's' || value[3] == 'S' {
		mode |= os.ModeSetuid
	}
	if value[6] == 's' || value[6] == 'S' {
		mode |= os.ModeSetgid
	}
	if value[9] == 't' || value[9] == 'T' {
		mode |= os.ModeSticky
	}
	return mode
}
