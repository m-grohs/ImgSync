package commands

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"imgsync/discover"
	"imgsync/state"
)

// bookmark formats supported by import and export. FormatAuto infers the
// format from the file extension, defaulting to txt.
const (
	formatAuto = "auto"
	formatTxt  = "txt"
	formatCSV  = "csv"
	formatJSON = "json"
)

const bookmarksFormatHeader = "# imgsync bookmarks v1"

var bookmarksVersionRe = regexp.MustCompile(`^#\s*imgsync bookmarks v(\d+)\s*$`)

// bookmarkEntry is one record in an import/export file. Pointer fields
// distinguish "not specified" (nil) from an explicit value, which matters for
// merge imports. The string "none" means "clear this field".
type bookmarkEntry struct {
	source    string
	URL       string
	Slug      string
	Canonical string
	SeriesID  string
	Assume    *string
	Except    *[]string
	Title     *string
	Location  *string
	Enabled   *bool
}

func resolveBookmarksFormat(explicit, filename string) (string, error) {
	switch explicit {
	case "", formatAuto:
	case formatTxt, formatCSV, formatJSON:
		return explicit, nil
	default:
		return "", fmt.Errorf("unknown format %q (want txt, csv or json)", explicit)
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv":
		return formatCSV, nil
	case ".json":
		return formatJSON, nil
	default:
		return formatTxt, nil
	}
}

func decodeBookmarks(data []byte, format string) ([]bookmarkEntry, []string, error) {
	var (
		entries  []bookmarkEntry
		warnings []string
		err      error
	)
	switch format {
	case formatTxt:
		entries, warnings, err = decodeBookmarksTxt(data)
	case formatCSV:
		entries, warnings, err = decodeBookmarksCSV(data)
	case formatJSON:
		entries, warnings, err = decodeBookmarksJSON(data)
	default:
		return nil, nil, fmt.Errorf("unknown format %q", format)
	}
	if err != nil {
		return nil, warnings, err
	}
	for i := range entries {
		if entries[i].source == "" {
			entries[i].source = fmt.Sprintf("bookmarks[%d]", i)
		}
		canonical, slug, parseErr := discover.ParseSeriesURL(entries[i].URL)
		if parseErr != nil {
			return nil, warnings, fmt.Errorf("%s: invalid series URL %q: %w", entries[i].source, entries[i].URL, parseErr)
		}
		entries[i].Canonical = canonical
		entries[i].Slug = slug
		entries[i].SeriesID = discover.SeriesID(slug)
	}
	return entries, warnings, nil
}

func encodeBookmarks(entries []bookmarkEntry, format string) ([]byte, error) {
	switch format {
	case formatTxt:
		return encodeBookmarksTxt(entries), nil
	case formatCSV:
		return encodeBookmarksCSV(entries)
	case formatJSON:
		return encodeBookmarksJSON(entries)
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
}

// -- txt ---------------------------------------------------------------------

func decodeBookmarksTxt(data []byte) ([]bookmarkEntry, []string, error) {
	var (
		entries  []bookmarkEntry
		warnings []string
	)
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if m := bookmarksVersionRe.FindStringSubmatch(line); m != nil && m[1] != "1" {
				return nil, warnings, fmt.Errorf("line %d: unsupported bookmarks version %s", i+1, m[1])
			}
			continue
		}
		source := fmt.Sprintf("line %d", i+1)
		tokens, err := tokenizeBookmarkLine(line)
		if err != nil {
			return nil, warnings, fmt.Errorf("%s: %w", source, err)
		}
		if len(tokens) == 0 {
			continue
		}
		e := bookmarkEntry{source: source, URL: tokens[0]}
		seen := map[string]bool{}
		for _, token := range tokens[1:] {
			key, value, ok := strings.Cut(token, "=")
			if !ok || key == "" {
				return nil, warnings, fmt.Errorf("%s: expected key=value, got %q", source, token)
			}
			if seen[key] {
				return nil, warnings, fmt.Errorf("%s: duplicate key %q", source, key)
			}
			seen[key] = true
			if err := setEntryField(&e, key, value, source); err != nil {
				if isUnknownBookmarkKey(err) {
					warnings = append(warnings, fmt.Sprintf("%s: ignoring unknown key %q", source, key))
					continue
				}
				return nil, warnings, err
			}
		}
		entries = append(entries, e)
	}
	return entries, warnings, nil
}

// tokenizeBookmarkLine splits on whitespace outside double quotes. Inside
// quotes, backslash escapes \" \\ \n and \t.
func tokenizeBookmarkLine(line string) ([]string, error) {
	var (
		tokens           []string
		cur              strings.Builder
		inToken, inQuote bool
		escaped          bool
	)
	for _, r := range line {
		switch {
		case escaped:
			switch r {
			case 'n':
				cur.WriteRune('\n')
			case 't':
				cur.WriteRune('\t')
			default:
				cur.WriteRune(r)
			}
			escaped = false
		case r == '\\' && inQuote:
			escaped = true
		case r == '"':
			inToken, inQuote = true, !inQuote
		case unicode.IsSpace(r) && !inQuote:
			if inToken {
				tokens = append(tokens, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteRune(r)
			inToken = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("trailing escape")
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	if inToken {
		tokens = append(tokens, cur.String())
	}
	return tokens, nil
}

func encodeBookmarksTxt(entries []bookmarkEntry) []byte {
	var b strings.Builder
	b.WriteString(bookmarksFormatHeader)
	b.WriteString("\n# url [assume=NONE] [except=RANGES] [title=\"...\"] [location=\"...\"] [enabled=0]\n")
	for _, e := range entries {
		fields := []string{e.URL}
		if e.Assume != nil {
			fields = append(fields, "assume="+quoteBookmarkValue(*e.Assume))
		}
		if e.Except != nil {
			fields = append(fields, "except="+quoteBookmarkValue(exceptValue(*e.Except)))
		}
		if e.Title != nil {
			fields = append(fields, "title="+quoteBookmarkValue(*e.Title))
		}
		if e.Location != nil {
			fields = append(fields, "location="+quoteBookmarkValue(*e.Location))
		}
		if e.Enabled != nil {
			value := "1"
			if !*e.Enabled {
				value = "0"
			}
			fields = append(fields, "enabled="+value)
		}
		b.WriteString(strings.Join(fields, " "))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func quoteBookmarkValue(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\r\n\"\\") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// -- csv ---------------------------------------------------------------------

var bookmarksCSVHeader = []string{"url", "assume", "except", "title", "location", "enabled"}

func decodeBookmarksCSV(data []byte) ([]bookmarkEntry, []string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read csv header: %w", err)
	}
	cols := map[string]int{}
	for i, name := range header {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		cols[name] = i
	}
	var warnings []string
	for _, name := range header {
		if !slices.Contains(bookmarksCSVHeader, strings.ToLower(strings.TrimSpace(name))) {
			warnings = append(warnings, fmt.Sprintf("ignoring unknown csv column %q", name))
		}
	}
	if _, ok := cols["url"]; !ok {
		return nil, warnings, fmt.Errorf("csv header is missing the url column")
	}
	readField := func(row []string, name string) string {
		i, ok := cols[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var entries []bookmarkEntry
	for rowNum := 2; ; rowNum++ {
		row, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, warnings, fmt.Errorf("csv row %d: %w", rowNum, err)
		}
		source := fmt.Sprintf("row %d", rowNum)
		e := bookmarkEntry{source: source, URL: readField(row, "url")}
		for _, field := range []struct {
			name string
			set  func(*bookmarkEntry, string, string) error
		}{
			{"assume", setAssumeField},
			{"except", setExceptField},
			{"title", setTitleField},
			{"location", setLocationField},
			{"enabled", setEnabledField},
		} {
			value := readField(row, field.name)
			if value == "" {
				continue
			}
			if err := field.set(&e, value, source); err != nil {
				return nil, warnings, err
			}
		}
		entries = append(entries, e)
	}
	return entries, warnings, nil
}

func encodeBookmarksCSV(entries []bookmarkEntry) ([]byte, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	if err := w.Write(bookmarksCSVHeader); err != nil {
		return nil, err
	}
	for _, e := range entries {
		row := make([]string, len(bookmarksCSVHeader))
		row[0] = e.URL
		if e.Assume != nil {
			row[1] = *e.Assume
		}
		if e.Except != nil {
			row[2] = exceptValue(*e.Except)
		}
		if e.Title != nil {
			row[3] = *e.Title
		}
		if e.Location != nil {
			row[4] = *e.Location
		}
		if e.Enabled != nil {
			if *e.Enabled {
				row[5] = "1"
			} else {
				row[5] = "0"
			}
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

// -- json --------------------------------------------------------------------

type bookmarkJSON struct {
	URL      string    `json:"url"`
	Assume   *string   `json:"assume,omitempty"`
	Except   *[]string `json:"except,omitempty"`
	Title    *string   `json:"title,omitempty"`
	Location *string   `json:"location,omitempty"`
	Enabled  *bool     `json:"enabled,omitempty"`
}

type bookmarksFileJSON struct {
	Format    string         `json:"format,omitempty"`
	Version   int            `json:"version,omitempty"`
	Bookmarks []bookmarkJSON `json:"bookmarks"`
}

func decodeBookmarksJSON(data []byte) ([]bookmarkEntry, []string, error) {
	var file bookmarksFileJSON
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, nil, fmt.Errorf("invalid json: %w", err)
	}
	if file.Format != "" && file.Format != "imgsync-bookmarks" {
		return nil, nil, fmt.Errorf("unsupported json format %q", file.Format)
	}
	if file.Version != 0 && file.Version != 1 {
		return nil, nil, fmt.Errorf("unsupported bookmarks version %d", file.Version)
	}
	entries := make([]bookmarkEntry, 0, len(file.Bookmarks))
	for i, b := range file.Bookmarks {
		source := fmt.Sprintf("bookmarks[%d]", i)
		e := bookmarkEntry{source: source, URL: b.URL}
		if b.Assume != nil {
			if err := setAssumeField(&e, *b.Assume, source); err != nil {
				return nil, nil, err
			}
		}
		if b.Except != nil {
			if err := setExceptItems(&e, *b.Except, source); err != nil {
				return nil, nil, err
			}
		}
		if b.Title != nil {
			if err := setTitleField(&e, *b.Title, source); err != nil {
				return nil, nil, err
			}
		}
		if b.Location != nil {
			if err := setLocationField(&e, *b.Location, source); err != nil {
				return nil, nil, err
			}
		}
		e.Enabled = b.Enabled
		entries = append(entries, e)
	}
	return entries, nil, nil
}

func encodeBookmarksJSON(entries []bookmarkEntry) ([]byte, error) {
	file := bookmarksFileJSON{Format: "imgsync-bookmarks", Version: 1}
	for _, e := range entries {
		file.Bookmarks = append(file.Bookmarks, bookmarkJSON{
			URL:      e.URL,
			Assume:   e.Assume,
			Except:   e.Except,
			Title:    e.Title,
			Location: e.Location,
			Enabled:  e.Enabled,
		})
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(file); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// -- field helpers -----------------------------------------------------------

type unknownBookmarkKeyError struct{ key string }

func (e unknownBookmarkKeyError) Error() string { return "unknown key " + e.key }

func isUnknownBookmarkKey(err error) bool {
	_, ok := err.(unknownBookmarkKeyError)
	return ok
}

func setEntryField(e *bookmarkEntry, key, value, source string) error {
	switch key {
	case "assume":
		return setAssumeField(e, value, source)
	case "except":
		return setExceptField(e, value, source)
	case "title":
		return setTitleField(e, value, source)
	case "location":
		return setLocationField(e, value, source)
	case "enabled":
		return setEnabledField(e, value, source)
	default:
		return unknownBookmarkKeyError{key}
	}
}

func setAssumeField(e *bookmarkEntry, value, source string) error {
	if strings.EqualFold(value, "none") {
		v := "none"
		e.Assume = &v
		return nil
	}
	number := discover.NormalizeChapter(value)
	f, err := strconv.ParseFloat(number, 64)
	if number == "" || err != nil || f <= 0 {
		return fmt.Errorf("%s: invalid assume chapter %q", source, value)
	}
	e.Assume = &number
	return nil
}

func setExceptField(e *bookmarkEntry, value, source string) error {
	if strings.EqualFold(value, "none") {
		empty := []string{}
		e.Except = &empty
		return nil
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s: empty except list (use 'none' to clear)", source)
	}
	return setExceptItems(e, strings.Split(value, ","), source)
}

func setExceptItems(e *bookmarkEntry, parts []string, source string) error {
	out := []string{}
	seen := map[string]bool{}
	add := func(number string) {
		if !seen[number] {
			seen[number] = true
			out = append(out, number)
		}
	}
	for _, raw := range parts {
		part := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "\r"))
		if part == "" {
			return fmt.Errorf("%s: empty chapter in except list", source)
		}
		if strings.Count(part, "-") == 1 && !strings.HasPrefix(part, "-") {
			bounds := strings.Split(part, "-")
			lo, errLo := strconv.Atoi(strings.TrimSpace(bounds[0]))
			hi, errHi := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if errLo != nil || errHi != nil || lo <= 0 || hi < lo || hi-lo > 10000 {
				return fmt.Errorf("%s: invalid chapter range %q", source, part)
			}
			for n := lo; n <= hi; n++ {
				add(discover.NormalizeChapter(strconv.Itoa(n)))
			}
			continue
		}
		number := discover.NormalizeChapter(part)
		f, err := strconv.ParseFloat(number, 64)
		if number == "" || err != nil || f <= 0 {
			return fmt.Errorf("%s: invalid except chapter %q", source, part)
		}
		add(number)
	}
	discover.SortChapters(out)
	e.Except = &out
	return nil
}

func setTitleField(e *bookmarkEntry, value, source string) error {
	v, err := clearableBookmarkString(value, "title", source)
	if err != nil {
		return err
	}
	e.Title = v
	return nil
}

func setLocationField(e *bookmarkEntry, value, source string) error {
	v, err := clearableBookmarkString(value, "location", source)
	if err != nil {
		return err
	}
	e.Location = v
	return nil
}

func clearableBookmarkString(value, key, source string) (*string, error) {
	if strings.EqualFold(value, "none") {
		v := "none"
		return &v, nil
	}
	if value == "" {
		return nil, fmt.Errorf("%s: empty %s (use 'none' to clear)", source, key)
	}
	return &value, nil
}

func setEnabledField(e *bookmarkEntry, value, source string) error {
	var enabled bool
	switch value {
	case "1":
		enabled = true
	case "0":
		enabled = false
	default:
		return fmt.Errorf("%s: invalid enabled value %q (want 0 or 1)", source, value)
	}
	e.Enabled = &enabled
	return nil
}

func exceptValue(chapters []string) string {
	if len(chapters) == 0 {
		return "none"
	}
	return strings.Join(chapters, ",")
}

func entryFromBookmark(b state.Bookmark, exceptions []string) bookmarkEntry {
	e := bookmarkEntry{
		URL:       b.URL,
		Slug:      b.Slug,
		Canonical: b.URL,
		SeriesID:  b.SeriesID,
	}
	if b.AssumeSyncedThrough != "" {
		v := b.AssumeSyncedThrough
		e.Assume = &v
	}
	if len(exceptions) > 0 {
		v := append([]string(nil), exceptions...)
		discover.SortChapters(v)
		e.Except = &v
	}
	if b.Title != "" {
		v := b.Title
		e.Title = &v
	}
	if b.Location != "" {
		v := b.Location
		e.Location = &v
	}
	enabled := b.Enabled
	e.Enabled = &enabled
	return e
}

func (e bookmarkEntry) toStateImport() state.ImportBookmark {
	return state.ImportBookmark{
		Slug:     e.Slug,
		URL:      e.Canonical,
		SeriesID: e.SeriesID,
		Assume:   e.Assume,
		Except:   e.Except,
		Title:    e.Title,
		Location: e.Location,
		Enabled:  e.Enabled,
	}
}
