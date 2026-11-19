package discover

import (
	"encoding/json"
	"html"
	"testing"
)

var testPage = map[string]any{
	"url":    []any{0.0, "https://cdn.example.com/ch/1/abc.webp?v=1"},
	"width":  []any{0.0, 900.0},
	"height": []any{0.0, 1600.0},
}

func chapterHTML(pages int, locked bool) string {
	list := make([]any, pages)
	for i := range list {
		list[i] = []any{0.0, testPage}
	}
	props := map[string]any{
		"pages":         []any{1.0, list},
		"isLocked":      []any{0.0, locked},
		"isPremium":     []any{0.0, false},
		"chapterNumber": []any{0.0, 1.0},
	}
	raw, _ := json.Marshal(props)
	return `<html><head><title>Series Chapter 1</title></head><body>` +
		`<astro-island component-url="/_astro/ChapterReader.x.js" props="` +
		html.EscapeString(string(raw)) + `"></astro-island></body></html>`
}

func TestParseSeriesURL(t *testing.T) {
	gotURL, gotSlug, err := ParseSeriesURL("https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7/?x=1")
	if err != nil {
		t.Fatal(err)
	}
	wantURL := "https://asurascans.com/comics/blade-of-the-discarded-7ba78ec7"
	if gotURL != wantURL {
		t.Fatalf("url = %q, want %q", gotURL, wantURL)
	}
	if gotSlug != "blade-of-the-discarded-7ba78ec7" {
		t.Fatalf("slug = %q", gotSlug)
	}
	if got := SeriesFolder(gotSlug); got != "blade of the discarded" {
		t.Fatalf("folder = %q, want %q", got, "blade of the discarded")
	}
}

func TestRejectsNonSeriesURL(t *testing.T) {
	for _, raw := range []string{
		"https://asurascans.com/comics/x/chapter/1",
		"https://example.com/foo/bar",
		"https://example.com/",
	} {
		if _, _, err := ParseSeriesURL(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestSeriesTitleKeepsHyphens(t *testing.T) {
	got := SeriesTitle(`<html><head><title>Re:Zero - Starting Life</title></head></html>`)
	if got != "Re:Zero - Starting Life" {
		t.Fatalf("SeriesTitle = %q", got)
	}
}

func TestSeriesFolderName(t *testing.T) {
	cases := []struct {
		title string
		slug  string
		want  string
	}{
		{
			"The Genius Knight of a Fallen Race",
			"the-genius-knight-of-a-fallen-race-7ba78ec7",
			"The Genius Knight of a Fallen Race",
		},
		{"  Spaced   Title  ", "slug", "Spaced Title"},
		{"Re:Zero - Starting Life", "re-zero", "Re Zero - Starting Life"},
		{"A/B: C?", "a-b-123456", "A B C"},
		{"", "blade-of-the-discarded-7ba78ec7", "blade of the discarded"},
		{" .. ", "blade-of-the-discarded-7ba78ec7", "blade of the discarded"},
	}
	for _, tc := range cases {
		if got := SeriesFolderName(tc.title, tc.slug); got != tc.want {
			t.Errorf("SeriesFolderName(%q, %q) = %q, want %q", tc.title, tc.slug, got, tc.want)
		}
	}
}

func TestDiscoverChaptersSortedAndDeduped(t *testing.T) {
	doc := `<a href="/comics/x-abc123/chapter/2"></a>` +
		`<a href="/comics/x-abc123/chapter/10"></a>` +
		`<a href="/comics/x-abc123/chapter/2"></a>`
	got := DiscoverChapters(doc)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Number != "002" || got[0].Path != "/comics/x-abc123/chapter/2" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Number != "010" || got[1].Path != "/comics/x-abc123/chapter/10" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestDiscoverChaptersFallbackText(t *testing.T) {
	doc := `<a href="/read/thing/1">Chapter 1: Start</a><a href="/read/thing/2">Chapter 2</a>`
	got := DiscoverChapters(doc)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Number != "001" || got[1].Number != "002" {
		t.Fatalf("numbers = %+v", got)
	}
}

func TestDiscoverChaptersMergesTextFallback(t *testing.T) {
	doc := `<a href="/comics/x/chapter/1">Chapter 1</a>` +
		`<a href="/read/thing/2">Chapter 2</a>`
	got := DiscoverChapters(doc)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Number != "001" || got[1].Number != "002" {
		t.Fatalf("numbers = %+v", got)
	}
}

func TestParseChapterCompactsIndices(t *testing.T) {
	list := []any{
		42.0,                                  // not an object
		map[string]any{"width": 1.0},          // no url
		testPage,                              // valid
		map[string]any{"url": []any{0.0, ""}}, // empty url
		testPage,                              // valid
	}
	raw, _ := json.Marshal(map[string]any{"pages": []any{1.0, list}})
	doc := `<astro-island component-url="/_astro/ChapterReader.x.js" props="` +
		html.EscapeString(string(raw)) + `"></astro-island>`
	chapter, err := ParseChapter(doc, "001", "https://site/chapter/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(chapter.Pages) != 2 || chapter.Pages[0].Index != 0 || chapter.Pages[1].Index != 1 {
		t.Fatalf("pages = %+v", chapter.Pages)
	}
}

func TestUnrelatedPropsDoNotBlockImageFallback(t *testing.T) {
	props := html.EscapeString(`{"pages":"not-an-array","chapterNumber":1}`)
	doc := `<astro-island component-url="/_astro/Foo.x.js" props="` + props + `"></astro-island>` +
		`<img src="/img/001.jpg">`
	chapter, err := ParseChapter(doc, "001", "https://site/chapter/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(chapter.Pages) != 1 || chapter.Pages[0].URL != "https://site/img/001.jpg" {
		t.Fatalf("pages = %+v", chapter.Pages)
	}
}

func TestChapterLabel(t *testing.T) {
	cases := map[string]string{
		"1":    "Chapter 001",
		"12.5": "Chapter 12.5",
	}
	for in, want := range cases {
		if got := ChapterLabel(in); got != want {
			t.Fatalf("ChapterLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChapterRangeLabel(t *testing.T) {
	cases := []struct {
		nums []string
		want string
	}{
		{nil, ""},
		{[]string{"125"}, "Chapter 125"},
		{[]string{"122", "123", "124", "125"}, "Chapter 122-125"},
		{[]string{"003", "001", "002"}, "Chapter 001-003"},
		{[]string{"001", "002", "004", "005", "006"}, "Chapter 001-002, 004-006"},
		{[]string{"12.5", "013"}, "Chapter 12.5, 013"},
		{[]string{"001", "001", "002"}, "Chapter 001-002"},
	}
	for _, tc := range cases {
		if got := ChapterRangeLabel(tc.nums); got != tc.want {
			t.Errorf("ChapterRangeLabel(%v) = %q, want %q", tc.nums, got, tc.want)
		}
	}
}

func TestNormalizeChapter(t *testing.T) {
	cases := map[string]string{
		"1":           "001",
		"001":         "001",
		"chapter 001": "001",
		"chapter 1":   "001",
		"1.5":         "1.5",
		"chapter 1.5": "1.5",
	}
	for in, want := range cases {
		if got := NormalizeChapter(in); got != want {
			t.Fatalf("NormalizeChapter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChapterAtOrBefore(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"001", "123", true},
		{"123", "123", true},
		{"124", "123", false},
		{"12.5", "12.5", true},
		{"12.6", "12.5", false},
	}
	for _, tc := range cases {
		if got := ChapterAtOrBefore(tc.a, tc.b); got != tc.want {
			t.Errorf("ChapterAtOrBefore(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDecodeAstro(t *testing.T) {
	got := decodeAstro(map[string]any{
		"a": []any{0.0, 1.0},
		"b": []any{1.0, []any{[]any{0.0, "x"}}},
	}).(map[string]any)
	if got["a"] != 1.0 {
		t.Fatalf("a = %#v", got["a"])
	}
	list, ok := got["b"].([]any)
	if !ok || len(list) != 1 || list[0] != "x" {
		t.Fatalf("b = %#v", got["b"])
	}
}

func TestParseChapterPages(t *testing.T) {
	chapter, err := ParseChapter(chapterHTML(2, false), "001", "u")
	if err != nil {
		t.Fatal(err)
	}
	if chapter.Locked {
		t.Fatal("unexpected locked chapter")
	}
	if len(chapter.Pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(chapter.Pages))
	}
	if chapter.Pages[0].Index != 0 || chapter.Pages[1].Index != 1 {
		t.Fatalf("indices = %+v", chapter.Pages)
	}
}

func TestEmptyWhenNoPages(t *testing.T) {
	chapter, err := ParseChapter(chapterHTML(0, false), "009", "u")
	if err != nil {
		t.Fatal(err)
	}
	if chapter.Locked || !chapter.Empty || chapter.Reason != "no pages" {
		t.Fatalf("locked=%v empty=%v reason=%q", chapter.Locked, chapter.Empty, chapter.Reason)
	}
}

func TestLockedPremiumFlag(t *testing.T) {
	chapter, err := ParseChapter(chapterHTML(1, true), "009", "u")
	if err != nil {
		t.Fatal(err)
	}
	if !chapter.Locked || chapter.Reason != "premium" {
		t.Fatalf("locked=%v reason=%q", chapter.Locked, chapter.Reason)
	}
}

func TestParseChapterFallbackImages(t *testing.T) {
	doc := `<html><body><img src="https://x/a.jpg"><img src="https://x/b.png"></body></html>`
	chapter, err := ParseChapter(doc, "001", "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(chapter.Pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(chapter.Pages))
	}
	if chapter.Pages[0].URL != "https://x/a.jpg" || chapter.Pages[1].URL != "https://x/b.png" {
		t.Fatalf("pages = %+v", chapter.Pages)
	}
}

func TestParseChapterFallbackResolvesRelativeImages(t *testing.T) {
	doc := `<html><body><img src="/img/001.jpg"></body></html>`
	chapter, err := ParseChapter(doc, "001", "https://site/comics/x/chapter/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(chapter.Pages) != 1 || chapter.Pages[0].URL != "https://site/img/001.jpg" {
		t.Fatalf("pages = %+v", chapter.Pages)
	}
}

func TestExtension(t *testing.T) {
	if got := PageExtension("https://x/a.JPG?v=1"); got != ".jpg" {
		t.Fatalf("got %q", got)
	}
	if got := PageExtension("https://x/a.bin"); got != ".webp" {
		t.Fatalf("got %q", got)
	}
}

func TestParseChapterFolderName(t *testing.T) {
	cases := map[string]string{
		"chapter 001": "001",
		"chapter-001": "001",
		"chapter 1.5": "1.5",
		"Chapter 12":  "012",
	}
	for in, want := range cases {
		got, ok := ParseChapterFolderName(in)
		if !ok || got != want {
			t.Fatalf("ParseChapterFolderName(%q) = %q,%v, want %q", in, got, ok, want)
		}
	}
	if _, ok := ParseChapterFolderName("extras"); ok {
		t.Fatal("unexpected match for extras")
	}
}

func TestParsePageFileName(t *testing.T) {
	cases := map[string]int{"001.webp": 0, "002.JPG": 1, "100.png": 99}
	for in, want := range cases {
		got, ok := ParsePageFileName(in)
		if !ok || got != want {
			t.Fatalf("ParsePageFileName(%q) = %d,%v, want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"001.txt", "readme.webp", "000.webp"} {
		if _, ok := ParsePageFileName(in); ok {
			t.Fatalf("unexpected match for %q", in)
		}
	}
}
