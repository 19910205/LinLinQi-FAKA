package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"linlinqi/api/internal/config"
)

func testStore(t *testing.T, environment string) *Store {
	t.Helper()
	return New(config.Config{
		Env: environment, StorageRoot: t.TempDir(), MediaPublicBaseURL: "https://media.example.test/media",
		MediaMaxImageBytes: 1 << 20, MediaStorageMaxBytes: 16 << 20, MediaMinFreeBytes: 0,
	})
}

func encodedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var output bytes.Buffer
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	picture.Set(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	if err := png.Encode(&output, picture); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return output.Bytes()
}

func TestPutImageValidatesAndDeduplicatesContent(t *testing.T) {
	store := testStore(t, "test")
	payload := encodedPNG(t, 4, 3)
	first, err := store.PutImage(bytes.NewReader(payload), "catalog.png")
	if err != nil {
		t.Fatalf("put image: %v", err)
	}
	second, err := store.PutImage(bytes.NewReader(payload), "duplicate.png")
	if err != nil {
		t.Fatalf("put duplicate image: %v", err)
	}
	if first.ObjectKey != second.ObjectKey || first.SHA256 != second.SHA256 || first.Width != 4 || first.Height != 3 || first.MIME != "image/png" {
		t.Fatalf("content-addressed media mismatch: first=%#v second=%#v", first, second)
	}
	if !strings.HasPrefix(first.PublicURL, "https://media.example.test/media/sha256/") {
		t.Fatalf("unexpected public URL: %s", first.PublicURL)
	}
}

func TestPutImageRejectsMarkupAndUnsafeDimensions(t *testing.T) {
	store := testStore(t, "test")
	if _, err := store.PutImage(strings.NewReader(`<svg><script>alert(1)</script></svg>`), "image.svg"); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("SVG was not rejected: %v", err)
	}
	if _, err := store.PutImage(bytes.NewReader(encodedPNG(t, 20_001, 1)), "wide.png"); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("unsafe dimensions were not rejected: %v", err)
	}
}

func TestFreeBytesThresholdHandlesMultiplicationOverflow(t *testing.T) {
	if !freeBytesBelowThreshold(1, 4096, 8192) {
		t.Fatal("low free-space value was not detected")
	}
	if freeBytesBelowThreshold(^uint64(0), ^uint64(0), 1<<62) {
		t.Fatal("overflowing free-space product was treated as a small value")
	}
}

func TestCapacityLockSerializesIndependentStoreInstances(t *testing.T) {
	store := testStore(t, "test")
	first, err := store.acquireCapacityLock()
	if err != nil {
		t.Fatalf("acquire first capacity lock: %v", err)
	}
	lockPath := filepath.Join(store.root, "media", ".capacity.lock")
	second, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		_ = first.Close()
		t.Fatalf("open second capacity lock handle: %v", err)
	}
	defer second.Close()
	if err := syscall.Flock(int(second.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		_ = first.Close()
		t.Fatal("a second store could enter the capacity-critical section")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release first capacity lock: %v", err)
	}
	if err := syscall.Flock(int(second.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("capacity lock was not released: %v", err)
	}
}

func TestMirrorImageRejectsProductionHTTPAndRedirects(t *testing.T) {
	payload := encodedPNG(t, 2, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(writer, request, "/image", http.StatusFound)
			return
		}
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	if _, err := testStore(t, "production").MirrorImage(context.Background(), server.URL+"/image"); err == nil {
		t.Fatal("production media mirror accepted HTTP/private source")
	}
	development := testStore(t, "test")
	if _, err := development.MirrorImage(context.Background(), server.URL+"/redirect"); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("media mirror followed or accepted redirect: %v", err)
	}
	object, err := development.MirrorImage(context.Background(), server.URL+"/image")
	if err != nil || object.Width != 2 || object.Height != 2 {
		t.Fatalf("valid development mirror failed: %#v err=%v", object, err)
	}
}
