// Package imagecache downloads and stores remote images (icons and
// screenshots) on disk for the frontend, which does not access the network.
package imagecache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MaxBytes limits the size of an image.
var MaxBytes int64 = 15 << 20

// ErrNotImage means the downloaded content is not an image.
var ErrNotImage = errors.New("content is not an image")

// Cache keeps images in Dir, named by the sha256 of the URL.
type Cache struct {
	Dir  string
	HTTP *http.Client

	mu       sync.Mutex
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	path string
	err  error
}

func (c *Cache) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// extFor picks the extension from the detected type.
func extFor(ctype string) string {
	switch ctype {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	}
	return ""
}

// sniff identifies the type from the bytes (never from the server header).
func sniff(b []byte) string {
	ct := http.DetectContentType(b)
	if strings.HasPrefix(ct, "image/") && extFor(ct) != "" {
		return ct
	}
	head := b
	if len(head) > 2048 {
		head = head[:2048]
	}
	if bytes.Contains(bytes.ToLower(head), []byte("<svg")) {
		return "image/svg+xml"
	}
	return ""
}

func key(u string) string {
	s := sha256.Sum256([]byte(u))
	return hex.EncodeToString(s[:])
}

// cached looks for an already downloaded file for the URL.
func (c *Cache) cached(k string) string {
	matches, _ := filepath.Glob(filepath.Join(c.Dir, k+".*"))
	for _, m := range matches {
		if !strings.Contains(filepath.Base(m), ".tmp") {
			return m
		}
	}
	return ""
}

// Get returns the local path of the image at rawURL, downloading it if needed.
// Concurrent requests for the same URL share a single download.
func (c *Cache) Get(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("invalid image URL: %q", rawURL)
	}
	k := key(rawURL)
	if p := c.cached(k); p != "" {
		return p, nil
	}

	c.mu.Lock()
	if c.inflight == nil {
		c.inflight = map[string]*call{}
	}
	if cl, ok := c.inflight[k]; ok {
		c.mu.Unlock()
		select {
		case <-cl.done:
			return cl.path, cl.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[k] = cl
	c.mu.Unlock()

	cl.path, cl.err = c.fetch(ctx, rawURL, k)
	close(cl.done)
	c.mu.Lock()
	delete(c.inflight, k)
	c.mu.Unlock()
	return cl.path, cl.err
}

func (c *Cache) fetch(ctx context.Context, rawURL, k string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "omastore")
	resp, err := c.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download image %s: HTTP %d", rawURL, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	if int64(len(b)) > MaxBytes {
		return "", fmt.Errorf("image larger than %d bytes", MaxBytes)
	}
	ct := sniff(b)
	if ct == "" {
		return "", fmt.Errorf("%w: %s", ErrNotImage, rawURL)
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(c.Dir, k+extFor(ct))
	tmp, err := os.CreateTemp(c.Dir, k+".tmp-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), final); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return final, nil
}
