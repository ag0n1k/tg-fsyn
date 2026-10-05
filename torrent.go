package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// maxBencodeDepth bounds list/dict nesting so a hostile .torrent cannot blow
// the stack. Real torrents nest four or five levels deep.
const maxBencodeDepth = 64

// TorrentFile is one file of a multi-file torrent.
type TorrentFile struct {
	Path   string // slash-separated, relative to the torrent's root folder
	Length int64
}

// TorrentMeta is the part of a .torrent we need to plan a season update.
type TorrentMeta struct {
	Name  string        // root folder (multi-file) or file name (single-file)
	Files []TorrentFile // nil for single-file torrents; BEP 47 padding files are dropped
}

// MultiFile reports whether the torrent has a root folder with files in it.
func (m *TorrentMeta) MultiFile() bool { return m.Files != nil }

// ParseTorrent decodes the metainfo of a .torrent file. Names and paths are
// validated to be plain path components, because they are later joined onto
// directories on the NAS and must not be able to escape them.
func ParseTorrent(data []byte) (*TorrentMeta, error) {
	d := &bdecoder{data: data}
	v, err := d.value(0)
	if err != nil {
		return nil, fmt.Errorf("bencode: %w", err)
	}
	root, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("torrent is not a dictionary")
	}
	info, ok := root["info"].(map[string]any)
	if !ok {
		return nil, errors.New("torrent has no info dictionary")
	}

	name := utf8Field(info, "name")
	if err := checkPathComponent(name); err != nil {
		return nil, fmt.Errorf("bad torrent name: %w", err)
	}
	meta := &TorrentMeta{Name: name}

	files, multi := info["files"].([]any)
	if !multi {
		if _, ok := info["length"].(int64); !ok {
			return nil, errors.New("torrent has neither files nor length")
		}
		return meta, nil
	}

	meta.Files = []TorrentFile{}
	for i, raw := range files {
		f, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("file %d is not a dictionary", i)
		}
		if attr, _ := f["attr"].(string); strings.Contains(attr, "p") {
			continue
		}
		length, ok := f["length"].(int64)
		if !ok || length < 0 {
			return nil, fmt.Errorf("file %d has no valid length", i)
		}
		parts, err := pathField(f)
		if err != nil {
			return nil, fmt.Errorf("file %d: %w", i, err)
		}
		meta.Files = append(meta.Files, TorrentFile{Path: strings.Join(parts, "/"), Length: length})
	}
	if len(meta.Files) == 0 {
		return nil, errors.New("torrent lists no files")
	}
	return meta, nil
}

// utf8Field prefers the "<key>.utf-8" variant some clients add next to a
// legacy-encoded "<key>".
func utf8Field(m map[string]any, key string) string {
	if s, ok := m[key+".utf-8"].(string); ok && s != "" {
		return s
	}
	s, _ := m[key].(string)
	return s
}

func pathField(f map[string]any) ([]string, error) {
	raw, ok := f["path.utf-8"].([]any)
	if !ok || len(raw) == 0 {
		raw, ok = f["path"].([]any)
	}
	if !ok || len(raw) == 0 {
		return nil, errors.New("missing path")
	}
	parts := make([]string, 0, len(raw))
	for _, p := range raw {
		s, ok := p.(string)
		if !ok {
			return nil, errors.New("path component is not a string")
		}
		if err := checkPathComponent(s); err != nil {
			return nil, err
		}
		parts = append(parts, s)
	}
	return parts, nil
}

func checkPathComponent(s string) error {
	switch {
	case s == "":
		return errors.New("empty path component")
	case s == "." || s == "..":
		return fmt.Errorf("path component %q is not allowed", s)
	case strings.ContainsAny(s, "/\\\x00"):
		return fmt.Errorf("path component %q contains a separator", s)
	}
	return nil
}

// bdecoder decodes bencode into int64, string (byte strings, binary-safe),
// []any and map[string]any.
type bdecoder struct {
	data []byte
	pos  int
}

func (d *bdecoder) value(depth int) (any, error) {
	if depth > maxBencodeDepth {
		return nil, errors.New("nesting too deep")
	}
	if d.pos >= len(d.data) {
		return nil, errors.New("unexpected end of data")
	}
	switch c := d.data[d.pos]; {
	case c == 'i':
		d.pos++
		end := d.indexFrom('e')
		if end < 0 {
			return nil, errors.New("unterminated integer")
		}
		n, err := strconv.ParseInt(string(d.data[d.pos:end]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad integer at %d: %w", d.pos, err)
		}
		d.pos = end + 1
		return n, nil
	case c == 'l':
		d.pos++
		list := []any{}
		for {
			if d.pos >= len(d.data) {
				return nil, errors.New("unterminated list")
			}
			if d.data[d.pos] == 'e' {
				d.pos++
				return list, nil
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
	case c == 'd':
		d.pos++
		dict := map[string]any{}
		for {
			if d.pos >= len(d.data) {
				return nil, errors.New("unterminated dictionary")
			}
			if d.data[d.pos] == 'e' {
				d.pos++
				return dict, nil
			}
			key, err := d.str()
			if err != nil {
				return nil, fmt.Errorf("dictionary key: %w", err)
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			dict[key] = v
		}
	case c >= '0' && c <= '9':
		return d.str()
	default:
		return nil, fmt.Errorf("unexpected byte %q at %d", c, d.pos)
	}
}

func (d *bdecoder) str() (string, error) {
	colon := d.indexFrom(':')
	if colon < 0 {
		return "", errors.New("unterminated string length")
	}
	n, err := strconv.Atoi(string(d.data[d.pos:colon]))
	if err != nil || n < 0 {
		return "", fmt.Errorf("bad string length at %d", d.pos)
	}
	start := colon + 1
	if n > len(d.data)-start {
		return "", errors.New("string runs past end of data")
	}
	d.pos = start + n
	return string(d.data[start:d.pos]), nil
}

func (d *bdecoder) indexFrom(b byte) int {
	for i := d.pos; i < len(d.data); i++ {
		if d.data[i] == b {
			return i
		}
	}
	return -1
}
