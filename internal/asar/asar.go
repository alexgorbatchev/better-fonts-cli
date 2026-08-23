package asar

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrFileNotFound = errors.New("file not found in asar archive")
	ErrInvalidAsar  = errors.New("invalid asar archive format")
)

// Entry represents a file, directory, or link inside the ASAR archive directory tree.
type Entry struct {
	Size       int64            `json:"size,omitempty"`
	Offset     string           `json:"offset,omitempty"`
	Executable bool             `json:"executable,omitempty"`
	Unpacked   bool             `json:"unpacked,omitempty"`
	Link       string           `json:"link,omitempty"`
	Files      map[string]Entry `json:"files,omitempty"`
}

// Archive represents an in-memory or mapped ASAR archive.
type Archive struct {
	Header        Entry
	HeaderSize    uint32
	PayloadOffset int64
	fileReader    io.ReaderAt
	closer        io.Closer
	rawPayload    []byte // cached or modified payload if in-memory
	filesContent  map[string][]byte
}

// OpenArchive opens an ASAR file for reading and extraction.
func OpenArchive(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening asar file %s: %w", path, err)
	}

	arch, err := readArchiveHeader(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	arch.fileReader = f
	arch.closer = f
	return arch, nil
}

// Close closes any underlying file handles.
func (a *Archive) Close() error {
	if a.closer != nil {
		return a.closer.Close()
	}
	return nil
}

func readArchiveHeader(r io.ReaderAt) (*Archive, error) {
	headerBuf := make([]byte, 16)
	if _, err := r.ReadAt(headerBuf, 0); err != nil {
		return nil, fmt.Errorf("reading asar 16-byte header: %w", err)
	}

	magic1 := binary.LittleEndian.Uint32(headerBuf[0:4])
	headerPayloadSize := binary.LittleEndian.Uint32(headerBuf[4:8])
	magic3 := binary.LittleEndian.Uint32(headerBuf[8:12])
	jsonLen := binary.LittleEndian.Uint32(headerBuf[12:16])

	if magic1 != 4 || magic3 != headerPayloadSize-4 {
		return nil, ErrInvalidAsar
	}

	jsonBuf := make([]byte, jsonLen)
	if _, err := r.ReadAt(jsonBuf, 16); err != nil {
		return nil, fmt.Errorf("reading asar JSON header: %w", err)
	}

	var root Entry
	if err := json.Unmarshal(jsonBuf, &root); err != nil {
		return nil, fmt.Errorf("decoding asar JSON header: %w", err)
	}

	payloadOffset := int64(8 + headerPayloadSize)

	return &Archive{
		Header:        root,
		HeaderSize:    headerPayloadSize,
		PayloadOffset: payloadOffset,
		filesContent:  make(map[string][]byte),
	}, nil
}

// ExtractFile extracts the contents of a single file from the archive.
func (a *Archive) ExtractFile(relPath string) ([]byte, error) {
	normPath := normalizePath(relPath)
	if cached, ok := a.filesContent[normPath]; ok {
		return cached, nil
	}

	entry, err := a.findEntry(normPath)
	if err != nil {
		return nil, err
	}

	if entry.Files != nil {
		return nil, fmt.Errorf("path %q is a directory, not a file", relPath)
	}
	if entry.Unpacked {
		return nil, fmt.Errorf("file %q is unpacked outside asar archive", relPath)
	}

	offset, err := strconv.ParseInt(entry.Offset, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid offset %q for %s: %w", entry.Offset, relPath, err)
	}

	buf := make([]byte, entry.Size)
	if a.rawPayload != nil {
		if offset+entry.Size > int64(len(a.rawPayload)) {
			return nil, fmt.Errorf("file %s out of bounds in raw payload", relPath)
		}
		copy(buf, a.rawPayload[offset:offset+entry.Size])
		return buf, nil
	}

	if a.fileReader == nil {
		return nil, errors.New("no reader available for asar payload")
	}

	if _, err := a.fileReader.ReadAt(buf, a.PayloadOffset+offset); err != nil {
		return nil, fmt.Errorf("reading file data for %s: %w", relPath, err)
	}

	return buf, nil
}

// HasFile reports whether the archive contains the specified file.
func (a *Archive) HasFile(relPath string) bool {
	normPath := normalizePath(relPath)
	_, err := a.findEntry(normPath)
	return err == nil
}

// ListFiles returns all relative file paths inside the archive.
func (a *Archive) ListFiles() []string {
	var list []string
	collectFiles(&a.Header, "", &list)
	sort.Strings(list)
	return list
}

func collectFiles(entry *Entry, prefix string, list *stringSlice) {
	for name, child := range entry.Files {
		full := name
		if prefix != "" {
			full = prefix + "/" + name
		}
		if child.Files != nil {
			collectFiles(&child, full, list)
		} else {
			*list = append(*list, full)
		}
	}
}

type stringSlice = []string

// ReplaceFile updates the in-memory content of a file in the archive.
func (a *Archive) ReplaceFile(relPath string, newContent []byte) error {
	normPath := normalizePath(relPath)
	if !a.HasFile(normPath) {
		return fmt.Errorf("cannot replace: %w: %s", ErrFileNotFound, relPath)
	}

	a.filesContent[normPath] = newContent
	return nil
}

// CreateArchive creates an in-memory ASAR archive from a map of file path -> content.
func CreateArchive(files map[string][]byte) (*Archive, error) {
	root := Entry{Files: make(map[string]Entry)}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var payload bytes.Buffer
	for _, rawKey := range keys {
		content := files[rawKey]
		normKey := normalizePath(rawKey)
		offsetStr := strconv.FormatInt(int64(payload.Len()), 10)
		size := int64(len(content))
		payload.Write(content)

		addEntryToTree(&root, normKey, Entry{
			Size:   size,
			Offset: offsetStr,
		})
	}

	return &Archive{
		Header:       root,
		rawPayload:   payload.Bytes(),
		filesContent: make(map[string][]byte),
	}, nil
}

// Save writes the archive (including any replaced files) to destPath.
func (a *Archive) Save(destPath string) error {
	allFiles := a.ListFiles()
	fileDataMap := make(map[string][]byte)

	for _, file := range allFiles {
		entry, err := a.findEntry(file)
		if err != nil {
			return err
		}
		if entry.Unpacked {
			continue
		}
		content, err := a.ExtractFile(file)
		if err != nil {
			return fmt.Errorf("extracting %s before saving: %w", file, err)
		}
		fileDataMap[file] = content
	}

	// Rebuild tree keeping unpacked markers intact
	newRoot := rebuildTreeWithUpdatedFiles(&a.Header, fileDataMap)

	var payload bytes.Buffer
	sort.Strings(allFiles)
	for _, file := range allFiles {
		if content, ok := fileDataMap[file]; ok {
			offsetStr := strconv.FormatInt(int64(payload.Len()), 10)
			updateOffsetInTree(&newRoot, file, offsetStr, int64(len(content)))
			payload.Write(content)
		}
	}

	jsonBytes, err := json.Marshal(newRoot)
	if err != nil {
		return fmt.Errorf("marshaling JSON header: %w", err)
	}

	jsonLen := uint32(len(jsonBytes))
	headerPayloadSize := (jsonLen + 8 + 3) & ^uint32(3)

	var header bytes.Buffer
	_ = binary.Write(&header, binary.LittleEndian, uint32(4))
	_ = binary.Write(&header, binary.LittleEndian, headerPayloadSize)
	_ = binary.Write(&header, binary.LittleEndian, headerPayloadSize-4)
	_ = binary.Write(&header, binary.LittleEndian, jsonLen)
	header.Write(jsonBytes)

	padding := int(headerPayloadSize - (jsonLen + 8))
	if padding > 0 {
		header.Write(make([]byte, padding))
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating dir for %s: %w", destPath, err)
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating output asar %s: %w", destPath, err)
	}
	defer outFile.Close()

	if _, err := outFile.Write(header.Bytes()); err != nil {
		return fmt.Errorf("writing asar header: %w", err)
	}
	if _, err := outFile.Write(payload.Bytes()); err != nil {
		return fmt.Errorf("writing asar payload: %w", err)
	}

	return nil
}

// PatchAsarFile reads targetRelPath from originalPath, applies transform, and saves to outputPath.
func PatchAsarFile(originalPath string, targetRelPath string, outputPath string, transform func(oldContent []byte) ([]byte, error)) error {
	arch, err := OpenArchive(originalPath)
	if err != nil {
		return err
	}
	defer arch.Close()

	oldBytes, err := arch.ExtractFile(targetRelPath)
	if err != nil {
		return fmt.Errorf("extracting %s from %s: %w", targetRelPath, originalPath, err)
	}

	newBytes, err := transform(oldBytes)
	if err != nil {
		return fmt.Errorf("transforming %s: %w", targetRelPath, err)
	}

	if err := arch.ReplaceFile(targetRelPath, newBytes); err != nil {
		return fmt.Errorf("replacing %s: %w", targetRelPath, err)
	}

	if err := arch.Save(outputPath); err != nil {
		return fmt.Errorf("saving patched asar to %s: %w", outputPath, err)
	}

	return nil
}

func (a *Archive) findEntry(relPath string) (*Entry, error) {
	parts := strings.Split(relPath, "/")
	current := &a.Header
	for i, part := range parts {
		if current.Files == nil {
			return nil, ErrFileNotFound
		}
		child, ok := current.Files[part]
		if !ok {
			return nil, ErrFileNotFound
		}
		if i == len(parts)-1 {
			return &child, nil
		}
		current = &child
	}
	return current, nil
}

func addEntryToTree(root *Entry, relPath string, leaf Entry) {
	parts := strings.Split(relPath, "/")
	current := root
	for i, part := range parts {
		if i == len(parts)-1 {
			current.Files[part] = leaf
			return
		}
		child, ok := current.Files[part]
		if !ok || child.Files == nil {
			child = Entry{Files: make(map[string]Entry)}
			current.Files[part] = child
		}
		current = &child
	}
}

func updateOffsetInTree(root *Entry, relPath string, newOffset string, newSize int64) {
	parts := strings.Split(relPath, "/")
	current := root
	for i, part := range parts {
		if i == len(parts)-1 {
			leaf := current.Files[part]
			leaf.Offset = newOffset
			leaf.Size = newSize
			current.Files[part] = leaf
			return
		}
		child := current.Files[part]
		updateOffsetInTree(&child, strings.Join(parts[i+1:], "/"), newOffset, newSize)
		current.Files[part] = child
		return
	}
}

func rebuildTreeWithUpdatedFiles(root *Entry, updatedFiles map[string][]byte) Entry {
	copyEntry := Entry{
		Size:       root.Size,
		Offset:     root.Offset,
		Executable: root.Executable,
		Unpacked:   root.Unpacked,
		Link:       root.Link,
	}
	if root.Files != nil {
		copyEntry.Files = make(map[string]Entry, len(root.Files))
		for k, v := range root.Files {
			copyEntry.Files[k] = rebuildTreeWithUpdatedFiles(&v, updatedFiles)
		}
	}
	return copyEntry
}

func normalizePath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	return p
}
