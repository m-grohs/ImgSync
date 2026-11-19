package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchImageConditional(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write([]byte("img"))
	}))
	defer srv.Close()

	f := New()
	defer f.Close()

	img, err := f.FetchImage(t.Context(), srv.URL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if img == nil || string(img.Body) != "img" || img.ETag != `"v1"` {
		t.Fatalf("img = %+v", img)
	}

	notModified, err := f.FetchImage(t.Context(), srv.URL, img.ETag, "")
	if err != nil {
		t.Fatal(err)
	}
	if notModified != nil {
		t.Fatalf("expected nil on 304, got %+v", notModified)
	}
}

func TestFetchImageRejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()

	f := New()
	defer f.Close()

	if _, err := f.FetchImage(t.Context(), srv.URL, "", ""); err == nil {
		t.Fatal("expected content-type error")
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", time.Now().Add(100*time.Millisecond).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := New()
	defer f.Close()
	start := time.Now()
	_, err := f.GetText(t.Context(), srv.URL)
	if err == nil {
		t.Fatal("expected final 429 to be returned as an error")
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("HTTP-date Retry-After was ignored (elapsed %s)", elapsed)
	}
}

func TestGetTextHonoursCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	f := New()
	defer f.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.GetText(ctx, srv.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline error, got %v", err)
	}
}

func TestGetText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	f := New()
	defer f.Close()

	text, err := f.GetText(t.Context(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" {
		t.Fatalf("text = %q", text)
	}
}
