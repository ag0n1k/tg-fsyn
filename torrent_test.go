package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// benc encodes int, int64, string, []any and map[string]any as bencode.
func benc(v any) []byte {
	var b strings.Builder
	var enc func(any)
	enc = func(v any) {
		switch x := v.(type) {
		case int:
			fmt.Fprintf(&b, "i%de", x)
		case int64:
			fmt.Fprintf(&b, "i%de", x)
		case string:
			fmt.Fprintf(&b, "%d:%s", len(x), x)
		case []any:
			b.WriteByte('l')
			for _, e := range x {
				enc(e)
			}
			b.WriteByte('e')
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('d')
			for _, k := range keys {
				enc(k)
				enc(x[k])
			}
			b.WriteByte('e')
		default:
			panic(fmt.Sprintf("benc: unsupported %T", v))
		}
	}
	enc(v)
	return []byte(b.String())
}

// multiFileTorrent builds a .torrent whose files are given as
// "path/inside" → length.
func multiFileTorrent(name string, files ...TorrentFile) []byte {
	list := []any{}
	for _, f := range files {
		parts := []any{}
		for _, p := range strings.Split(f.Path, "/") {
			parts = append(parts, p)
		}
		list = append(list, map[string]any{"length": f.Length, "path": parts})
	}
	return benc(map[string]any{
		"announce": "http://tracker.invalid/announce",
		"info": map[string]any{
			"name":         name,
			"piece length": 1 << 20,
			"pieces":       strings.Repeat("x", 20),
			"files":        list,
		},
	})
}

func TestParseTorrentMultiFile(t *testing.T) {
	data := multiFileTorrent("Show.S01",
		TorrentFile{"Show.S01E01.mkv", 100},
		TorrentFile{"Subs/Show.S01E01.srt", 7},
	)
	meta, err := ParseTorrent(data)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "Show.S01" || !meta.MultiFile() {
		t.Fatalf("got name %q multi=%v", meta.Name, meta.MultiFile())
	}
	want := []TorrentFile{{"Show.S01E01.mkv", 100}, {"Subs/Show.S01E01.srt", 7}}
	if fmt.Sprint(meta.Files) != fmt.Sprint(want) {
		t.Errorf("files = %v, want %v", meta.Files, want)
	}
}

func TestParseTorrentSingleFile(t *testing.T) {
	data := benc(map[string]any{"info": map[string]any{"name": "Movie.mkv", "length": 42}})
	meta, err := ParseTorrent(data)
	if err != nil {
		t.Fatal(err)
	}
	if meta.MultiFile() || meta.Name != "Movie.mkv" {
		t.Errorf("got %+v", meta)
	}
}

func TestParseTorrentPrefersUTF8AndSkipsPadding(t *testing.T) {
	data := benc(map[string]any{"info": map[string]any{
		"name":       "legacy",
		"name.utf-8": "Сериал.S01",
		"files": []any{
			map[string]any{"length": 5, "path": []any{"x"}, "path.utf-8": []any{"Серия 1.mkv"}},
			map[string]any{"length": 3, "path": []any{".pad", "3"}, "attr": "p"},
		},
	}})
	meta, err := ParseTorrent(data)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "Сериал.S01" {
		t.Errorf("name = %q", meta.Name)
	}
	if len(meta.Files) != 1 || meta.Files[0].Path != "Серия 1.mkv" {
		t.Errorf("files = %v", meta.Files)
	}
}

func TestParseTorrentRejectsEscapingPaths(t *testing.T) {
	for _, tc := range []struct {
		name    string
		torrent []byte
	}{
		{"dotdot name", benc(map[string]any{"info": map[string]any{"name": "..", "length": 1}})},
		{"slash in name", benc(map[string]any{"info": map[string]any{"name": "a/b", "length": 1}})},
		{"dotdot path", multiFileTorrent("S", TorrentFile{"../../etc/passwd", 1})},
		{"empty component", multiFileTorrent("S", TorrentFile{"a//b", 1})},
		{"backslash", multiFileTorrent("S", TorrentFile{`..\x`, 1})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseTorrent(tc.torrent); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestParseTorrentMalformed(t *testing.T) {
	deep := strings.Repeat("l", maxBencodeDepth+2) + strings.Repeat("e", maxBencodeDepth+2)
	for _, tc := range []struct{ name, data string }{
		{"empty", ""},
		{"not a dict", "i1e"},
		{"no info", "d1:ai1ee"},
		{"truncated string", "d4:info5:abce"},
		{"unterminated dict", "d4:infod4:name1:x"},
		{"bad integer", "d4:infod6:lengthi1x2e4:name1:xee"},
		{"huge string length", "d4:info99999999999:x"},
		{"no files no length", "d4:infod4:name1:xee"},
		{"empty file list", "d4:infod5:filesle4:name1:xee"},
		{"too deep", deep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseTorrent([]byte(tc.data)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
