package commands

import (
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func equalPtr[T comparable](got, want *T) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func roundTripEntries() []bookmarkEntry {
	return []bookmarkEntry{
		{
			URL:      "https://site.example/comics/alpha-abc123",
			Assume:   ptr("127"),
			Except:   ptr([]string{"104", "110", "111"}),
			Title:    ptr(`Alpha "A" & Co`),
			Location: ptr("~/Pictures/Alpha"),
			Enabled:  ptr(true),
		},
		{
			URL:     "https://site.example/comics/beta-def456",
			Enabled: ptr(false),
		},
	}
}

func assertEntriesMatch(t *testing.T, got, want []bookmarkEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.URL != w.URL || g.Slug == "" || g.Canonical == "" {
			t.Errorf("entry %d identity = %+v", i, g)
		}
		if !equalPtr(g.Assume, w.Assume) || !equalPtr(g.Title, w.Title) ||
			!equalPtr(g.Location, w.Location) || !equalPtr(g.Enabled, w.Enabled) {
			t.Errorf("entry %d fields = assume %v title %v location %v enabled %v; want %v",
				i, g.Assume, g.Title, g.Location, g.Enabled, w)
		}
		if (g.Except == nil) != (w.Except == nil) {
			t.Errorf("entry %d except presence = %v, want %v", i, g.Except, w.Except)
			continue
		}
		if g.Except != nil && !reflect.DeepEqual(*g.Except, *w.Except) {
			t.Errorf("entry %d except = %v, want %v", i, *g.Except, *w.Except)
		}
	}
}

func TestBookmarksFormatsRoundTrip(t *testing.T) {
	want := roundTripEntries()
	for _, format := range []string{formatTxt, formatCSV, formatJSON} {
		t.Run(format, func(t *testing.T) {
			data, err := encodeBookmarks(want, format)
			if err != nil {
				t.Fatal(err)
			}
			got, warnings, err := decodeBookmarks(data, format)
			if err != nil {
				t.Fatal(err)
			}
			if len(warnings) != 0 {
				t.Fatalf("warnings = %v", warnings)
			}
			assertEntriesMatch(t, got, want)
		})
	}
}

func TestDecodeBookmarksTxt(t *testing.T) {
	input := `# imgsync bookmarks v1
# a human note

https://site.example/comics/alpha-abc123 assume=12.5 except=104,110-111 title="Alpha \"A\" & Co" location="~/Pictures/Alpha" enabled=0
`
	entries, warnings, err := decodeBookmarks([]byte(input), formatTxt)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	want := []bookmarkEntry{{
		URL:      "https://site.example/comics/alpha-abc123",
		Assume:   ptr("12.5"),
		Except:   ptr([]string{"104", "110", "111"}),
		Title:    ptr(`Alpha "A" & Co`),
		Location: ptr("~/Pictures/Alpha"),
		Enabled:  ptr(false),
	}}
	assertEntriesMatch(t, entries, want)
}

func TestDecodeBookmarksTxtClearValuesAndWarnings(t *testing.T) {
	input := `https://site.example/comics/alpha-abc123 assume=none except=none title=none location=none future=value
https://site.example/comics/beta-def456
`
	entries, warnings, err := decodeBookmarks([]byte(input), formatTxt)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "future") {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if !equalPtr(entries[0].Assume, ptr("none")) || !equalPtr(entries[0].Title, ptr("none")) ||
		!equalPtr(entries[0].Location, ptr("none")) {
		t.Fatalf("clear markers = %+v", entries[0])
	}
	if entries[0].Except == nil || len(*entries[0].Except) != 0 {
		t.Fatalf("except = %v, want empty", entries[0].Except)
	}
	if entries[1].Assume != nil || entries[1].Enabled != nil {
		t.Fatalf("second entry should have absent fields: %+v", entries[1])
	}
}

func TestDecodeBookmarksTxtErrors(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"version", "# imgsync bookmarks v2\nhttps://site.example/comics/a-abc123\n", "version"},
		{"quote", "https://site.example/comics/a-abc123 title=\"oops\n", "quote"},
		{"key value", "https://site.example/comics/a-abc123 nonsense\n", "key=value"},
		{"duplicate", "https://site.example/comics/a-abc123 assume=1 assume=2\n", "duplicate"},
		{"assume", "https://site.example/comics/a-abc123 assume=0\n", "assume"},
		{"range", "https://site.example/comics/a-abc123 except=200-100\n", "range"},
		{"enabled", "https://site.example/comics/a-abc123 enabled=yes\n", "enabled"},
		{"url", "https://example.com/not-a-series assume=1\n", "URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := decodeBookmarks([]byte(tt.input), formatTxt)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeBookmarksCSV(t *testing.T) {
	input := `url,assume,except,title,location,enabled,future
https://site.example/comics/alpha-abc123,127,"104,110-111",Alpha,~/Pictures/Alpha,0,x
https://site.example/comics/beta-def456,,,,,,
`
	entries, warnings, err := decodeBookmarks([]byte(input), formatCSV)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "future") {
		t.Fatalf("warnings = %v", warnings)
	}
	want := []bookmarkEntry{
		{
			URL:      "https://site.example/comics/alpha-abc123",
			Assume:   ptr("127"),
			Except:   ptr([]string{"104", "110", "111"}),
			Title:    ptr("Alpha"),
			Location: ptr("~/Pictures/Alpha"),
			Enabled:  ptr(false),
		},
		{URL: "https://site.example/comics/beta-def456"},
	}
	assertEntriesMatch(t, entries, want)
}

func TestDecodeBookmarksCSVErrors(t *testing.T) {
	for _, input := range []string{
		"assume,title\n1,X\n",
		"url,assume\nhttps://site.example/comics/a-abc123,\"unterminated\n",
		"url,assume\nhttps://site.example/comics/a-abc123,abc\n",
	} {
		if _, _, err := decodeBookmarks([]byte(input), formatCSV); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestDecodeBookmarksJSON(t *testing.T) {
	input := `{
  "format": "imgsync-bookmarks",
  "version": 1,
  "bookmarks": [
    {"url": "https://site.example/comics/alpha-abc123", "assume": "127", "except": ["104", "110-111"], "title": "Alpha", "location": "~/Pictures/Alpha", "enabled": false},
    {"url": "https://site.example/comics/beta-def456", "assume": "none", "except": [], "enabled": true}
  ]
}`
	entries, warnings, err := decodeBookmarks([]byte(input), formatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	want := []bookmarkEntry{
		{
			URL:      "https://site.example/comics/alpha-abc123",
			Assume:   ptr("127"),
			Except:   ptr([]string{"104", "110", "111"}),
			Title:    ptr("Alpha"),
			Location: ptr("~/Pictures/Alpha"),
			Enabled:  ptr(false),
		},
		{
			URL:     "https://site.example/comics/beta-def456",
			Assume:  ptr("none"),
			Except:  ptr([]string{}),
			Enabled: ptr(true),
		},
	}
	assertEntriesMatch(t, entries, want)
}

func TestDecodeBookmarksJSONErrors(t *testing.T) {
	for _, input := range []string{
		`{"format":"other","bookmarks":[]}`,
		`{"version":2,"bookmarks":[]}`,
		`{"bookmarks":[{"url":"https://example.com/nope"}]}`,
		`{"bookmarks":`,
	} {
		if _, _, err := decodeBookmarks([]byte(input), formatJSON); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestResolveBookmarksFormat(t *testing.T) {
	tests := []struct {
		explicit, filename, want string
		wantErr                  bool
	}{
		{"auto", "bookmarks.csv", formatCSV, false},
		{"auto", "bookmarks.JSON", formatJSON, false},
		{"auto", "bookmarks.txt", formatTxt, false},
		{"auto", "-", formatTxt, false},
		{"json", "bookmarks.csv", formatJSON, false},
		{"yaml", "bookmarks.csv", "", true},
	}
	for _, tt := range tests {
		got, err := resolveBookmarksFormat(tt.explicit, tt.filename)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("resolveBookmarksFormat(%q, %q) = %q, %v", tt.explicit, tt.filename, got, err)
		}
	}
}

func TestEncodeBookmarksTxtHeader(t *testing.T) {
	data, err := encodeBookmarks(roundTripEntries(), formatTxt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), bookmarksFormatHeader+"\n") {
		t.Fatalf("txt export should start with a version header: %q", data)
	}
}
