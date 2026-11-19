package fetch

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
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
	maxRetryDelay      = 60 * time.Second
)

var retryStatuses = map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}

var commonHeaders = map[string]string{
	"User-Agent":      userAgent,
	"Accept-Language": "en-US,en;q=0.9",
}

var documentHeaders = map[string]string{
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
	"Sec-CH-UA":                 `"Chromium";v="131", "Not_A Brand";v="24"`,
	"Sec-CH-UA-Mobile":          "?0",
	"Sec-CH-UA-Platform":        `"Linux"`,
	"Sec-Fetch-Dest":            "document",
	"Sec-Fetch-Mode":            "navigate",
	"Sec-Fetch-Site":            "none",
	"Sec-Fetch-User":            "?1",
	"Upgrade-Insecure-Requests": "1",
}

var imageHeaders = map[string]string{
	"Accept":         "image/avif,image/webp,image/apng,image/*,*/*;q=0.8",
	"Sec-Fetch-Dest": "image",
	"Sec-Fetch-Mode": "no-cors",
}

// ImageResponse is a fetched image.
type ImageResponse struct {
	Body         []byte
	ContentType  string
	ETag         string
	LastModified string
}

// Fetcher is a polite HTTP client: browser headers, HTTP/2, retries and
// throttling. Every request is bound to the context passed by the caller.
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

// throttle spaces requests globally and honours cancellation while waiting.
func (f *Fetcher) throttle(ctx context.Context) error {
	f.throttleMu.Lock()
	defer f.throttleMu.Unlock()
	wait := f.minInterval - time.Since(f.lastRequest)
	if wait > 0 {
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
	f.lastRequest = time.Now()
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func backoff(attempt int) time.Duration {
	base := time.Duration(1<<attempt) * time.Second
	return base + time.Duration(rand.Int64N(int64(time.Second)))
}

func retryDelay(resp *http.Response, attempt int) time.Duration {
	delay := backoff(attempt)
	ra := resp.Header.Get("Retry-After")
	if ra == "" {
		return delay
	}
	if secs, err := strconv.Atoi(ra); err == nil {
		delay = time.Duration(secs) * time.Second
	} else if when, err := http.ParseTime(ra); err == nil {
		delay = time.Until(when)
	}
	if delay < 0 {
		delay = 0
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}

func (f *Fetcher) do(ctx context.Context, rawURL string, extra map[string]string) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		if err := f.throttle(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range commonHeaders {
			req.Header.Set(k, v)
		}
		for k, v := range extra {
			req.Header.Set(k, v)
		}

		resp, err := f.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt >= maxAttempts {
				return nil, fmt.Errorf("%w: %s", err, rawURL)
			}
			delay := backoff(attempt)
			slog.Warn("network error, retrying", "error", err, "delay", delay.String(), "url", rawURL)
			if err := sleepCtx(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}

		if retryStatuses[resp.StatusCode] && attempt < maxAttempts {
			delay := retryDelay(resp, attempt)
			_ = resp.Body.Close()
			slog.Warn("retryable HTTP status, retrying", "status", resp.StatusCode, "delay", delay.String(), "url", rawURL)
			if err := sleepCtx(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}

		if resp.StatusCode == http.StatusForbidden {
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
func (f *Fetcher) GetText(ctx context.Context, rawURL string) (string, error) {
	resp, err := f.do(ctx, rawURL, documentHeaders)
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
// 304 Not Modified. The concurrency slot is held until the body is fully read.
func (f *Fetcher) FetchImage(ctx context.Context, rawURL, etag, lastModified string) (*ImageResponse, error) {
	headers := maps.Clone(imageHeaders)
	if etag != "" {
		headers["If-None-Match"] = etag
	}
	if lastModified != "" {
		headers["If-Modified-Since"] = lastModified
	}

	f.sem <- struct{}{}
	defer func() { <-f.sem }()

	resp, err := f.do(ctx, rawURL, headers)
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
		LastModified: resp.Header.Get("LastModified"),
	}, nil
}
