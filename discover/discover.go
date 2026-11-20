package discover

import (
	"cmp"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	seriesRe      = regexp.MustCompile(`(?i)^/(?:comics?|series|manga|read)/([^/]+)/?$`)
	validSlugRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	hashSuffixRe  = regexp.MustCompile(`-[0-9a-f]{6,12}$`)
	ogTitleRe     = regexp.MustCompile(`(?i)<meta[^>]+property="og:title"[^>]+content="([^"]*)"`)
	titleRe       = regexp.MustCompile(`(?i)<title>([^<]*)</title>`)
	asuraSuffixRe = regexp.MustCompile(`(?i)\s*[-|–—]\s*Asura Scans\s*$`)

	astroIslandRe = regexp.MustCompile(`(?is)<astro-island\b[^>]*>`)
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
	Locked bool // premium/locked by the site
	Empty  bool // page payload was readable but contained no images
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
	if m == nil || !validSlugRe.MatchString(m[1]) {
		return "", "", fmt.Errorf("not a series URL - expected https://<site>/comics/<series-slug> (or /series/, /manga/)")
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

// ChapterRangeLabel formats chapter numbers as compact labels:
// ["001","002","003","005"] -> "Chapter 001-003, 005".
func ChapterRangeLabel(nums []string) string {
	if len(nums) == 0 {
		return ""
	}
	sorted := append([]string(nil), nums...)
	SortChapters(sorted)

	unique := sorted[:0]
	for _, number := range sorted {
		if len(unique) == 0 || unique[len(unique)-1] != number {
			unique = append(unique, number)
		}
	}

	parts := make([]string, 0, len(unique))
	for i := 0; i < len(unique); {
		start, ok := chapterInt(unique[i])
		if !ok {
			parts = append(parts, chapterBody(unique[i]))
			i++
			continue
		}
		end := start
		j := i
		for j+1 < len(unique) {
			next, ok := chapterInt(unique[j+1])
			if !ok || next != end+1 {
				break
			}
			end = next
			j++
		}
		if j > i {
			parts = append(parts, fmt.Sprintf("%03d-%03d", start, end))
			i = j + 1
			continue
		}
		parts = append(parts, chapterBody(unique[i]))
		i++
	}
	return "Chapter " + strings.Join(parts, ", ")
}

// chapterBody is the chapter number as shown after the "Chapter " prefix.
func chapterBody(number string) string {
	if f, err := strconv.ParseFloat(number, 64); err == nil && f == math.Trunc(f) {
		return fmt.Sprintf("%03d", int64(f))
	}
	return number
}

// chapterInt returns the chapter number as an int when it is a whole number.
func chapterInt(number string) (int, bool) {
	f, err := strconv.ParseFloat(number, 64)
	if err != nil || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}

// ChapterCompare orders chapters numerically, falling back to lexical order
// for non-numeric values.
func ChapterCompare(a, b string) int {
	fa, ea := strconv.ParseFloat(a, 64)
	fb, eb := strconv.ParseFloat(b, 64)
	if ea == nil && eb == nil && !math.IsNaN(fa) && !math.IsNaN(fb) {
		return cmp.Compare(fa, fb)
	}
	return cmp.Compare(a, b)
}

// ChapterLess orders chapters numerically.
func ChapterLess(a, b string) bool { return ChapterCompare(a, b) < 0 }

// ChapterAtOrBefore reports whether a is less than or equal to b.
func ChapterAtOrBefore(a, b string) bool {
	fa, ea := strconv.ParseFloat(a, 64)
	fb, eb := strconv.ParseFloat(b, 64)
	if ea == nil && eb == nil && !math.IsNaN(fa) && !math.IsNaN(fb) {
		return fa <= fb
	}
	return a <= b
}

// SortChapters sorts chapter numbers ascending.
func SortChapters(nums []string) { slices.SortFunc(nums, ChapterCompare) }

// DiscoverChapters returns chapter numbers and paths sorted ascending.
func DiscoverChapters(doc string) []ChapterRef {
	byNumber := map[string]string{}
	seenPath := map[string]bool{}
	for _, m := range hrefRe.FindAllStringSubmatch(doc, -1) {
		if cm := chapterPathRe.FindStringSubmatch(m[1]); cm != nil {
			num := NormalizeChapter(cm[1])
			if _, ok := byNumber[num]; !ok {
				byNumber[num] = m[1]
			}
			seenPath[m[1]] = true
		}
	}
	// Merge link-text matches instead of only using them as an all-or-nothing
	// fallback: a page may mix /chapter/N links with text-only chapter links.
	// A link with a usable /chapter/N path is already authoritative; parsing its
	// text as well can mistake an adjacent date ("Chapter 1481 day ago") for a
	// chapter number. Replacing tags with spaces keeps such text separated.
	for _, m := range anchorRe.FindAllStringSubmatch(doc, -1) {
		if seenPath[m[1]] {
			continue
		}
		text := strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(m[2], " "))), " ")
		if cm := chapterTextRe.FindStringSubmatch(text); cm != nil {
			num := NormalizeChapter(cm[1])
			if _, ok := byNumber[num]; !ok {
				byNumber[num] = m[1]
				seenPath[m[1]] = true
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
	var fallback map[string]any
	for _, island := range astroIslandRe.FindAllString(doc, -1) {
		m := propsRe.FindStringSubmatch(island)
		if m == nil {
			continue
		}
		obj := decodeProps(m[1])
		if !hasPagesArray(obj) {
			continue
		}
		if strings.Contains(strings.ToLower(island), "chapterreader") {
			return obj
		}
		if fallback == nil {
			fallback = obj
		}
	}
	if fallback != nil {
		return fallback
	}
	// Older markup may not identify the island; accept any props that contain
	// a real pages array rather than an unrelated "pages" key.
	for _, m := range propsRe.FindAllStringSubmatch(doc, -1) {
		obj := decodeProps(m[1])
		if hasPagesArray(obj) {
			return obj
		}
	}
	return nil
}

func decodeProps(raw string) map[string]any {
	var v any
	if err := json.Unmarshal([]byte(html.UnescapeString(raw)), &v); err != nil {
		return nil
	}
	obj, _ := v.(map[string]any)
	return obj
}

func hasPagesArray(obj map[string]any) bool {
	if obj == nil {
		return false
	}
	_, ok := obj["pages"].([]any)
	return ok
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
	pages := make([]Page, 0, len(rawPages))
	for _, rp := range rawPages {
		pm, ok := rp.(map[string]any)
		if !ok {
			continue
		}
		u, ok := pm["url"].(string)
		if !ok || u == "" {
			continue
		}
		// Compact indices so filenames stay contiguous after invalid entries.
		pages = append(pages, Page{Index: len(pages), URL: resolveAgainst(chapterURL, u)})
	}

	premium := truthy(props["isLocked"]) || truthy(props["isPremium"])
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
		Locked: premium,
		Empty:  !premium && len(pages) == 0,
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
	slices.SortFunc(hits, func(a, b hit) int { return cmp.Compare(a.start, b.start) })
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
