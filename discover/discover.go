package discover

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	seriesRe      = regexp.MustCompile(`^/(?:[^/]+/)?([^/]+)/?$`)
	hashSuffixRe  = regexp.MustCompile(`-[0-9a-f]{6,12}$`)
	ogTitleRe     = regexp.MustCompile(`(?i)<meta[^>]+property="og:title"[^>]+content="([^"]*)"`)
	titleRe       = regexp.MustCompile(`(?i)<title>([^<]*)</title>`)
	asuraSuffixRe = regexp.MustCompile(`(?i)\s*\|\s*Asura Scans\s*$`)

	propsRe       = regexp.MustCompile(`props="([^"]*)"`)
	hrefRe        = regexp.MustCompile(`href="([^"]+)"`)
	anchorRe      = regexp.MustCompile(`(?is)<a\b[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	chapterPathRe = regexp.MustCompile(`(?i)(?:^|[/_-])chapter[-/]([0-9]+(?:\.[0-9]+)?)(?:$|[/?#._-])`)
	chapterTextRe = regexp.MustCompile(`(?i)^\s*chapter\s*[:.]?\s*([0-9]+(?:\.[0-9]+)?)\b`)
	tagRe         = regexp.MustCompile(`<[^>]*>`)

	imgTagRe   = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	aTagRe     = regexp.MustCompile(`(?i)<a\b[^>]*>`)
	srcRe      = regexp.MustCompile(`(?i)(?:^|\s)src="([^"]+)"`)
	dataSrcRe  = regexp.MustCompile(`(?i)(?:^|\s)data-src="([^"]+)"`)
	srcsetRe   = regexp.MustCompile(`(?i)(?:^|\s)srcset="([^"]+)"`)
	hrefAttrRe = regexp.MustCompile(`(?i)(?:^|\s)href="([^"]+)"`)

	chapterFolderRe = regexp.MustCompile(`(?i)^chapter[\s_-]+([0-9]+(?:\.[0-9]+)?)\s*$`)
	pageFileRe      = regexp.MustCompile(`^(\d+)\.([A-Za-z0-9]+)$`)

	folderUnsafeRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
)

var imageExtensions = map[string]bool{
	".webp": true, ".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".avif": true,
}

// Page is a single image inside a chapter.
type Page struct {
	Index int
	URL   string
}

// Chapter is a parsed chapter page.
type Chapter struct {
	Number string
	URL    string
	Locked bool
	Reason string
	Pages  []Page
}

// ChapterRef is a discovered chapter link.
type ChapterRef struct {
	Number string
	Path   string
}

// ParseSeriesURL returns the canonical URL and slug for a series page URL.
func ParseSeriesURL(raw string) (canonical, slug string, err error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	p := strings.TrimSuffix(u.Path, "/")
	m := seriesRe.FindStringSubmatch(p)
	if m == nil {
		return "", "", fmt.Errorf("not a series URL - expected https://<site>/comics/<series-slug>")
	}
	return u.Scheme + "://" + u.Host + p, m[1], nil
}

// SeriesFolder is the slug-derived fallback folder name: trailing site-id hash
// stripped and dashes converted to spaces.
func SeriesFolder(slug string) string {
	s := hashSuffixRe.ReplaceAllString(slug, "")
	if s == "" {
		s = slug
	}
	return strings.ReplaceAll(s, "-", " ")
}

// SeriesFolderName is the on-disk folder for a series. It keeps the title's
// original capitalization and falls back to the slug-derived name when the
// title is empty or unusable.
func SeriesFolderName(title, slug string) string {
	if name := sanitizeFolderName(title); name != "" {
		return name
	}
	return SeriesFolder(slug)
}

func sanitizeFolderName(title string) string {
	name := folderUnsafeRe.ReplaceAllString(title, " ")
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, " .")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

// SeriesID returns the trailing site hash of a slug, or "" when absent.
func SeriesID(slug string) string {
	if m := hashSuffixRe.FindString(slug); m != "" {
		return m[1:]
	}
	return ""
}

// SeriesTitle extracts the page title, strips the Asura Scan suffix and
// converts dashes to spaces.
func SeriesTitle(doc string) string {
	var title string
	if m := ogTitleRe.FindStringSubmatch(doc); m != nil {
		title = m[1]
	} else if m := titleRe.FindStringSubmatch(doc); m != nil {
		title = m[1]
	}
	title = strings.TrimSpace(html.UnescapeString(title))
	title = asuraSuffixRe.ReplaceAllString(title, "")
	title = strings.ReplaceAll(title, "-", " ")
	return strings.TrimSpace(title)
}

// NormalizeChapter canonicalizes a chapter number: "1"/"001"/"chapter 001"
// becomes "001"; fractional numbers keep their form ("1.5").
func NormalizeChapter(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "chapter") {
		s = strings.TrimSpace(s[len("chapter"):])
	}
	if s == "" {
		return ""
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	if f == math.Trunc(f) {
		return fmt.Sprintf("%03d", int64(f))
	}
	return s
}

// ChapterLabel is the chapter folder name: "Chapter 001", "Chapter 12.5".
func ChapterLabel(number string) string {
	f, err := strconv.ParseFloat(number, 64)
	if err == nil && f == math.Trunc(f) {
		return fmt.Sprintf("Chapter %03d", int64(f))
	}
	return "Chapter " + number
}

// ChapterLess orders chapters numerically.
func ChapterLess(a, b string) bool {
	fa, ea := strconv.ParseFloat(a, 64)
	fb, eb := strconv.ParseFloat(b, 64)
	if ea == nil && eb == nil {
		return fa < fb
	}
	return a < b
}

// SortChapters sorts chapter numbers ascending.
func SortChapters(nums []string) {
	sort.Slice(nums, func(i, j int) bool { return ChapterLess(nums[i], nums[j]) })
}

// DiscoverChapters returns chapter numbers and paths sorted ascending.
func DiscoverChapters(doc string) []ChapterRef {
	byNumber := map[string]string{}
	for _, m := range hrefRe.FindAllStringSubmatch(doc, -1) {
		if cm := chapterPathRe.FindStringSubmatch(m[1]); cm != nil {
			num := NormalizeChapter(cm[1])
			if _, ok := byNumber[num]; !ok {
				byNumber[num] = m[1]
			}
		}
	}
	if len(byNumber) == 0 {
		for _, m := range anchorRe.FindAllStringSubmatch(doc, -1) {
			text := strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(m[2], "")))
			if cm := chapterTextRe.FindStringSubmatch(text); cm != nil {
				num := NormalizeChapter(cm[1])
				if _, ok := byNumber[num]; !ok {
					byNumber[num] = m[1]
				}
			}
		}
	}
	nums := make([]string, 0, len(byNumber))
	for n := range byNumber {
		nums = append(nums, n)
	}
	SortChapters(nums)
	out := make([]ChapterRef, 0, len(nums))
	for _, n := range nums {
		out = append(out, ChapterRef{Number: n, Path: byNumber[n]})
	}
	return out
}

// PageExtension returns the lower-cased image extension, or ".webp" when the
// URL has no known image extension.
func PageExtension(raw string) string {
	p := raw
	if u, err := url.Parse(raw); err == nil {
		p = u.Path
	}
	ext := strings.ToLower(path.Ext(p))
	if imageExtensions[ext] {
		return ext
	}
	return ".webp"
}

func resolveAgainst(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func hasImageExt(raw string) bool {
	p := raw
	if u, err := url.Parse(raw); err == nil {
		p = u.Path
	}
	return imageExtensions[strings.ToLower(path.Ext(p))]
}

func htmlTitle(doc string) string {
	if m := titleRe.FindStringSubmatch(doc); m != nil {
		return html.UnescapeString(m[1])
	}
	return ""
}

func findChapterData(doc string) map[string]any {
	for _, m := range propsRe.FindAllStringSubmatch(doc, -1) {
		var v any
		if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &v); err != nil {
			continue
		}
		if obj, ok := v.(map[string]any); ok {
			if _, ok := obj["pages"]; ok {
				return obj
			}
			if _, ok := obj["chapterNumber"]; ok {
				return obj
			}
		}
	}
	return nil
}

func decodeAstro(v any) any {
	switch x := v.(type) {
	case []any:
		if len(x) == 2 {
			if tag, ok := x[0].(float64); ok && (int(tag) == 0 || int(tag) == 1) {
				if int(tag) == 0 {
					return decodeAstro(x[1])
				}
				if list, ok := x[1].([]any); ok {
					out := make([]any, len(list))
					for i, item := range list {
						out[i] = decodeAstro(item)
					}
					return out
				}
				return decodeAstro(x[1])
			}
		}
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = decodeAstro(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = decodeAstro(item)
		}
		return out
	default:
		return v
	}
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// ParseChapter extracts the page list from a chapter page. It first looks for
// Astro component props (Asura-style sites) and falls back to ordered image
// URLs in the document for other sites.
func ParseChapter(doc, number, chapterURL string) (*Chapter, error) {
	data := findChapterData(doc)
	title := htmlTitle(doc)

	if data == nil {
		if imgs := orderedImageURLs(doc); len(imgs) > 0 {
			pages := make([]Page, len(imgs))
			for i, u := range imgs {
				pages[i] = Page{Index: i, URL: resolveAgainst(chapterURL, u)}
			}
			return &Chapter{Number: number, URL: chapterURL, Pages: pages}, nil
		}
		if strings.Contains(strings.ToLower(title), "premium") {
			return &Chapter{Number: number, URL: chapterURL, Locked: true, Reason: "premium"}, nil
		}
		return nil, fmt.Errorf("no chapter payload found in %s", chapterURL)
	}

	props, _ := decodeAstro(data).(map[string]any)
	rawPages, _ := props["pages"].([]any)
	pages := []Page{}
	for i, rp := range rawPages {
		pm, ok := rp.(map[string]any)
		if !ok {
			continue
		}
		u, ok := pm["url"].(string)
		if !ok || u == "" {
			continue
		}
		pages = append(pages, Page{Index: i, URL: resolveAgainst(chapterURL, u)})
	}

	premium := truthy(props["isLocked"]) || truthy(props["isPremium"])
	locked := premium || len(pages) == 0
	reason := ""
	switch {
	case premium:
		reason = "premium"
	case len(pages) == 0:
		reason = "no pages"
	}
	return &Chapter{
		Number: number,
		URL:    chapterURL,
		Locked: locked,
		Reason: reason,
		Pages:  pages,
	}, nil
}

// orderedImageURLs collects image URLs from <img>/<a> tags in document order.
func orderedImageURLs(doc string) []string {
	type hit struct {
		start int
		url   string
	}
	var hits []hit
	for _, loc := range imgTagRe.FindAllStringIndex(doc, -1) {
		tag := doc[loc[0]:loc[1]]
		if u := firstAttr(tag, srcRe); u != "" {
			if hasImageExt(u) {
				hits = append(hits, hit{loc[0], u})
			}
			continue
		}
		if u := firstAttr(tag, dataSrcRe); u != "" {
			if hasImageExt(u) {
				hits = append(hits, hit{loc[0], u})
			}
			continue
		}
		if ss := firstAttr(tag, srcsetRe); ss != "" {
			if u := firstSrcsetURL(ss); u != "" && hasImageExt(u) {
				hits = append(hits, hit{loc[0], u})
			}
		}
	}
	for _, loc := range aTagRe.FindAllStringIndex(doc, -1) {
		tag := doc[loc[0]:loc[1]]
		if u := firstAttr(tag, hrefAttrRe); u != "" && hasImageExt(u) {
			hits = append(hits, hit{loc[0], u})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].start < hits[j].start })
	seen := map[string]bool{}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		if seen[h.url] {
			continue
		}
		seen[h.url] = true
		out = append(out, h.url)
	}
	return out
}

func firstAttr(tag string, re *regexp.Regexp) string {
	if m := re.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	return ""
}

func firstSrcsetURL(ss string) string {
	first := strings.Split(ss, ",")[0]
	fields := strings.Fields(first)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// ParseChapterFolderName parses "chapter 001" / "chapter-001" -> ("001", true).
func ParseChapterFolderName(name string) (string, bool) {
	m := chapterFolderRe.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}
	num := NormalizeChapter(m[1])
	if num == "" {
		return "", false
	}
	return num, true
}

// ParsePageFileName parses "001.webp" -> (0, true).
func ParsePageFileName(name string) (int, bool) {
	m := pageFileRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	if !imageExtensions["."+strings.ToLower(m[2])] {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
}
