package fetch

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

	img, err := f.FetchImage(srv.URL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if img == nil || string(img.Body) != "img" || img.ETag != `"v1"` {
		t.Fatalf("img = %+v", img)
	}

	notModified, err := f.FetchImage(srv.URL, img.ETag, "")
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

	if _, err := f.FetchImage(srv.URL, "", ""); err == nil {
		t.Fatal("expected content-type error")
	}
}

func TestGetText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	f := New()
	defer f.Close()

	text, err := f.GetText(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" {
		t.Fatalf("text = %q", text)
	}
}
