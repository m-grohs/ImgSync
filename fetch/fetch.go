package fetch

import (
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	userAgent          = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	maxAttempts        = 4
	defaultConcurrency = 3
	defaultMinInterval = 300 * time.Millisecond
)

var retryStatuses = map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}

var browserHeaders = map[string]string{
	"User-Agent":                userAgent,
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
	"Accept-Language":           "en-US,en;q=0.9",
	"Sec-CH-UA":                 `"Chromium";v="131", "Not_A Brand";v="24"`,
	"Sec-CH-UA-Mobile":          "?0",
	"Sec-CH-UA-Platform":        `"Linux"`,
	"Sec-Fetch-Dest":            "document",
	"Sec-Fetch-Mode":            "navigate",
	"Sec-Fetch-Site":            "none",
	"Sec-Fetch-User":            "?1",
	"Upgrade-Insecure-Requests": "1",
}

// ImageResponse is a fetched image.
type ImageResponse struct {
	Body         []byte
	ContentType  string
	ETag         string
	LastModified string
}

// Fetcher is a polite HTTP client: browser headers, HTTP/2, retries and
// throttling.
type Fetcher struct {
	client      *http.Client
	sem         chan struct{}
	throttleMu  sync.Mutex
	lastRequest time.Time
	minInterval time.Duration
}

// New builds a Fetcher with the default throttle settings.
func New() *Fetcher {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   20 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   20 * time.Second,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &Fetcher{
		client:      &http.Client{Transport: transport, Timeout: 60 * time.Second},
		sem:         make(chan struct{}, defaultConcurrency),
		minInterval: defaultMinInterval,
	}
}

// Close releases idle connections.
func (f *Fetcher) Close() {
	f.client.CloseIdleConnections()
}

func (f *Fetcher) throttle() {
	f.throttleMu.Lock()
	defer f.throttleMu.Unlock()
	wait := f.minInterval - time.Since(f.lastRequest)
	if wait > 0 {
		time.Sleep(wait)
	}
	f.lastRequest = time.Now()
}

func backoff(attempt int) time.Duration {
	base := time.Duration(1<<attempt) * time.Second
	return base + time.Duration(rand.Int64N(int64(time.Second)))
}

func (f *Fetcher) do(rawURL string, extra http.Header) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		f.throttle()

		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range browserHeaders {
			req.Header.Set(k, v)
		}
		for k, vv := range extra {
			for _, v := range vv {
				req.Header.Set(k, v)
			}
		}

		resp, err := f.client.Do(req)
		if err != nil {
			if attempt >= maxAttempts {
				return nil, fmt.Errorf("%w: %s", err, rawURL)
			}
			delay := backoff(attempt)
			slog.Warn("network error, retrying", "error", err, "delay", delay.String(), "url", rawURL)
			time.Sleep(delay)
			continue
		}

		if retryStatuses[resp.StatusCode] && attempt < maxAttempts {
			delay := backoff(attempt)
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil {
					delay = time.Duration(secs) * time.Second
				}
			}
			_ = resp.Body.Close()
			slog.Warn("retryable HTTP status, retrying", "status", resp.StatusCode, "delay", delay.String(), "url", rawURL)
			time.Sleep(delay)
			continue
		}

		if resp.StatusCode == 403 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf(
				"HTTP 403 (blocked - the site may require different headers or is challenging automated clients): %s",
				rawURL,
			)
		}

		return resp, nil
	}
}

// GetText fetches an HTML page.
func (f *Fetcher) GetText(rawURL string) (string, error) {
	resp, err := f.do(rawURL, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// FetchImage downloads an image. It returns (nil, nil) when the server answers
// 304 Not Modified.
func (f *Fetcher) FetchImage(rawURL, etag, lastModified string) (*ImageResponse, error) {
	headers := http.Header{}
	if etag != "" {
		headers.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		headers.Set("If-Modified-Since", lastModified)
	}

	f.sem <- struct{}{}
	resp, err := f.do(rawURL, headers)
	<-f.sem
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, rawURL)
	}

	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if contentType != "" && !strings.HasPrefix(contentType, "image/") && contentType != "application/octet-stream" {
		return nil, fmt.Errorf("unexpected content-type %q: %s", contentType, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &ImageResponse{
		Body:         body,
		ContentType:  contentType,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}
