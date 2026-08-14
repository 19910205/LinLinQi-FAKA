package media

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/bits"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/security"
)

var (
	ErrInvalidImage       = errors.New("invalid image")
	ErrStorageUnavailable = errors.New("media storage unavailable")
	objectNamePattern     = regexp.MustCompile(`^[a-f0-9]{64}\.(?:jpg|png|gif|webp)$`)
)

type Store struct {
	root, publicBase string
	maxImageBytes    int64
	maxStorageBytes  int64
	minFreeBytes     int64
	allowPrivate     bool
}

type Object struct {
	ObjectKey string
	PublicURL string
	FileName  string
	MIME      string
	Size      int64
	SHA256    string
	Width     int
	Height    int
}

func New(cfg config.Config) *Store {
	return &Store{
		root: cfg.StorageRoot, publicBase: strings.TrimRight(cfg.MediaPublicBaseURL, "/"),
		maxImageBytes: cfg.MediaMaxImageBytes, maxStorageBytes: cfg.MediaStorageMaxBytes,
		minFreeBytes: cfg.MediaMinFreeBytes, allowPrivate: cfg.Env != "production",
	}
}

func (s *Store) objectsRoot() string {
	return filepath.Join(s.root, "media", "objects", "sha256")
}

func (s *Store) stagingRoot() string {
	return filepath.Join(s.root, "media", "staging")
}

func (s *Store) Ensure() error {
	for _, directory := range []string{s.objectsRoot(), s.stagingRoot(), filepath.Join(s.root, "media", "quarantine"), filepath.Join(s.root, "mirror", "objects"), filepath.Join(s.root, "spool", "protocol-sync"), filepath.Join(s.root, "tmp")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("%w: create directory: %v", ErrStorageUnavailable, err)
		}
		// #nosec G302 -- this is a private directory; owner execute is required
		// to traverse it and the stored objects themselves remain mode 0600.
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("%w: secure directory: %v", ErrStorageUnavailable, err)
		}
	}
	return nil
}

func (s *Store) checkCapacity() error {
	if err := s.Ready(); err != nil {
		return err
	}
	var used int64
	if err := filepath.WalkDir(s.objectsRoot(), func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() < 0 || used > s.maxStorageBytes || info.Size() > s.maxStorageBytes-used {
				return fmt.Errorf("%w: object usage exceeds configured quota", ErrStorageUnavailable)
			}
			used += info.Size()
		}
		return nil
	}); err != nil {
		return fmt.Errorf("%w: inspect object usage: %v", ErrStorageUnavailable, err)
	}
	if used > s.maxStorageBytes || s.maxImageBytes > s.maxStorageBytes-used {
		return fmt.Errorf("%w: storage quota reached", ErrStorageUnavailable)
	}
	return nil
}

func (s *Store) acquireCapacityLock() (*os.File, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(s.root, "media", ".capacity.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: open capacity lock: %v", ErrStorageUnavailable, err)
	}
	if err := lock.Chmod(0o600); err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, errors.Join(fmt.Errorf("%w: lock media capacity: %v", ErrStorageUnavailable, err), lock.Close())
	}
	return lock, nil
}

func (s *Store) Ready() error {
	if err := s.Ensure(); err != nil {
		return err
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(s.root, &stats); err != nil {
		return fmt.Errorf("%w: stat filesystem: %v", ErrStorageUnavailable, err)
	}
	if stats.Bsize <= 0 {
		return fmt.Errorf("%w: invalid filesystem block size", ErrStorageUnavailable)
	}
	// Bsize is checked positive before conversion. Linux exposes it as int64,
	// while Darwin exposes uint32, so this conversion is safe on both targets.
	blockSize := uint64(stats.Bsize) // #nosec G115 -- positivity checked above
	if freeBytesBelowThreshold(stats.Bavail, blockSize, s.minFreeBytes) {
		return fmt.Errorf("%w: free space below safety threshold", ErrStorageUnavailable)
	}
	probe, err := os.CreateTemp(s.stagingRoot(), ".ready-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create readiness probe: %v", ErrStorageUnavailable, err)
	}
	probePath := probe.Name()
	defer os.Remove(probePath)
	if err := probe.Chmod(0o600); err != nil {
		return errors.Join(err, probe.Close())
	}
	if _, err := probe.Write([]byte("linlinqi-storage-ready")); err != nil {
		return errors.Join(fmt.Errorf("%w: write readiness probe: %v", ErrStorageUnavailable, err), probe.Close())
	}
	if err := probe.Sync(); err != nil {
		return errors.Join(fmt.Errorf("%w: sync readiness probe: %v", ErrStorageUnavailable, err), probe.Close())
	}
	if err := probe.Close(); err != nil {
		return err
	}
	return nil
}

func freeBytesBelowThreshold(blocks, blockSize uint64, minimum int64) bool {
	if minimum <= 0 {
		return false
	}
	high, low := bits.Mul64(blocks, blockSize)
	if high != 0 {
		// An overflowing product represents more than MaxUint64 bytes and
		// therefore cannot be below any positive int64 threshold.
		return false
	}
	return low < uint64(minimum) // #nosec G115 -- minimum is checked positive
}

func detectedImage(header []byte) (mime, extension string, ok bool) {
	detected := http.DetectContentType(header)
	switch detected {
	case "image/jpeg":
		return detected, "jpg", true
	case "image/png":
		return detected, "png", true
	case "image/gif":
		return detected, "gif", true
	case "image/webp":
		return detected, "webp", true
	default:
		return "", "", false
	}
}

func validateDimensions(reader io.Reader, mime string) (int, int, error) {
	if mime == "image/webp" {
		header := make([]byte, 64)
		if _, err := io.ReadFull(reader, header); err != nil || string(header[:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
			return 0, 0, ErrInvalidImage
		}
		width, height, ok := webpDimensions(header)
		if !ok || width < 1 || height < 1 || width > 20000 || height > 20000 || int64(width)*int64(height) > 100_000_000 {
			return 0, 0, ErrInvalidImage
		}
		return width, height, nil
	}
	decoded, _, err := image.DecodeConfig(reader)
	if err != nil || decoded.Width < 1 || decoded.Height < 1 || decoded.Width > 20000 || decoded.Height > 20000 || int64(decoded.Width)*int64(decoded.Height) > 100_000_000 {
		return 0, 0, ErrInvalidImage
	}
	return decoded.Width, decoded.Height, nil
}

// webpDimensions reads dimensions from the first WebP frame. All supported
// WebP variants (VP8X, VP8 and VP8L) keep this information in the first
// 30 bytes after the RIFF header, so no unbounded decode is needed here.
func webpDimensions(header []byte) (int, int, bool) {
	if len(header) < 20 || string(header[:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
		return 0, 0, false
	}
	chunk := string(header[12:16])
	payload := header[20:]
	switch chunk {
	case "VP8X":
		if len(payload) < 10 {
			return 0, 0, false
		}
		width := 1 + int(payload[4]) | int(payload[5])<<8 | int(payload[6])<<16
		height := 1 + int(payload[7]) | int(payload[8])<<8 | int(payload[9])<<16
		return width, height, true
	case "VP8 ":
		// Lossy VP8 frame header: start code 9d 01 2a, then 14-bit width/height.
		if len(payload) < 10 || payload[3] != 0x9d || payload[4] != 0x01 || payload[5] != 0x2a {
			return 0, 0, false
		}
		width := int(payload[6]) | int(payload[7])<<8
		height := int(payload[8]) | int(payload[9])<<8
		return width & 0x3fff, height & 0x3fff, true
	case "VP8L":
		if len(payload) < 5 || payload[0] != 0x2f {
			return 0, 0, false
		}
		bits := uint32(payload[1]) | uint32(payload[2])<<8 | uint32(payload[3])<<16 | uint32(payload[4])<<24
		width := int(bits&0x3fff) + 1
		height := int((bits>>14)&0x3fff) + 1
		return width, height, true
	default:
		return 0, 0, false
	}
}

func safeFileName(value, fallback string) string {
	value = strings.TrimSpace(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
	if value == "." || value == "/" || value == "" || len([]byte(value)) > 255 || strings.IndexFunc(value, func(character rune) bool { return character < 0x20 || character == 0x7f }) >= 0 {
		return fallback
	}
	return value
}

func (s *Store) PutImage(reader io.Reader, fileName string) (object Object, resultErr error) {
	capacityLock, err := s.acquireCapacityLock()
	if err != nil {
		return Object{}, err
	}
	// Closing releases flock even if publication fails. Surface a close error
	// only when it is the sole failure so callers can retry the idempotent put.
	defer func() {
		if closeErr := capacityLock.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("%w: release capacity lock: %v", ErrStorageUnavailable, closeErr)
		}
	}()
	if err := s.checkCapacity(); err != nil {
		return Object{}, err
	}
	temporary, err := os.CreateTemp(s.stagingRoot(), "image-*.part")
	if err != nil {
		return Object{}, fmt.Errorf("%w: create staging object: %v", ErrStorageUnavailable, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		return Object{}, errors.Join(err, temporary.Close())
	}
	buffered := bufio.NewReader(io.LimitReader(reader, s.maxImageBytes+1))
	header, _ := buffered.Peek(512)
	mime, extension, valid := detectedImage(header)
	if !valid {
		return Object{}, errors.Join(ErrInvalidImage, temporary.Close())
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), buffered)
	if copyErr != nil || written < 1 || written > s.maxImageBytes {
		return Object{}, errors.Join(ErrInvalidImage, temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return Object{}, errors.Join(fmt.Errorf("%w: sync staging object: %v", ErrStorageUnavailable, err), temporary.Close())
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return Object{}, errors.Join(fmt.Errorf("%w: seek staging object: %v", ErrStorageUnavailable, err), temporary.Close())
	}
	width, height, err := validateDimensions(temporary, mime)
	if err != nil {
		return Object{}, errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return Object{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	objectName := digest + "." + extension
	prefix := digest[:2]
	directory := filepath.Join(s.objectsRoot(), prefix)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Object{}, fmt.Errorf("%w: create object shard: %v", ErrStorageUnavailable, err)
	}
	// #nosec G302 -- owner execute is required to traverse this private shard.
	if err := os.Chmod(directory, 0o700); err != nil {
		return Object{}, err
	}
	destination := filepath.Join(directory, objectName)
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		if renameErr := os.Rename(temporaryPath, destination); renameErr != nil && !errors.Is(renameErr, os.ErrExist) {
			return Object{}, fmt.Errorf("%w: publish object: %v", ErrStorageUnavailable, renameErr)
		}
		// A concurrent mirror may have published the same digest first. In that
		// case the existing immutable object is the successful deduplicated result.
		if chmodErr := os.Chmod(destination, 0o600); chmodErr != nil && !errors.Is(chmodErr, os.ErrNotExist) {
			return Object{}, chmodErr
		}
	} else if err != nil {
		return Object{}, err
	}
	objectKey := "media/objects/sha256/" + prefix + "/" + objectName
	return Object{
		ObjectKey: objectKey, PublicURL: s.publicBase + "/sha256/" + prefix + "/" + objectName,
		FileName: safeFileName(fileName, objectName), MIME: mime, Size: written, SHA256: digest,
		Width: width, Height: height,
	}, nil
}

func (s *Store) MirrorImage(ctx context.Context, rawURL string) (Object, error) {
	parsed, err := security.ValidateOutboundURL(ctx, strings.TrimSpace(rawURL), s.allowPrivate)
	if err != nil {
		return Object{}, err
	}
	client := security.NewOutboundHTTPClient(20*time.Second, s.allowPrivate)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Object{}, err
	}
	request.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif;q=0.8")
	request.Header.Set("User-Agent", "LinLinQi-MediaMirror/1.0")
	response, err := client.Do(request)
	if err != nil {
		return Object{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.ContentLength > s.maxImageBytes {
		return Object{}, ErrInvalidImage
	}
	name := "remote-image"
	if unescaped, unescapeErr := url.PathUnescape(filepath.Base(parsed.Path)); unescapeErr == nil {
		name = safeFileName(unescaped, name)
	}
	return s.PutImage(response.Body, name)
}

func (s *Store) ResolveObject(prefix, fileName string) (string, string, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	fileName = strings.ToLower(strings.TrimSpace(fileName))
	if len(prefix) != 2 || !strings.HasPrefix(fileName, prefix) || !objectNamePattern.MatchString(fileName) {
		return "", "", os.ErrNotExist
	}
	path := filepath.Join(s.objectsRoot(), prefix, fileName)
	relative, err := filepath.Rel(s.objectsRoot(), path)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", "", os.ErrNotExist
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", os.ErrNotExist
	}
	extension := strings.TrimPrefix(filepath.Ext(fileName), ".")
	mime := map[string]string{"jpg": "image/jpeg", "png": "image/png", "gif": "image/gif", "webp": "image/webp"}[extension]
	if mime == "" {
		return "", "", os.ErrNotExist
	}
	return path, mime, nil
}
