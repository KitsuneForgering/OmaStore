package install

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Download limits.
var (
	maxDownloadBytes int64 = 2 << 30
	maxSmallBytes    int64 = 5 << 20 // checksums and icons
)

// ErrChecksum means the downloaded file does not match the published checksum.
var ErrChecksum = errors.New("checksum mismatch")

func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL %q is not https", raw)
	}
	return nil
}

func (in *Installer) get(ctx context.Context, raw string) (*http.Response, error) {
	if err := checkURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "omastore")
	resp, err := httpsOnly(in.http()).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", raw, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %d", raw, resp.StatusCode)
	}
	return resp, nil
}

// download writes raw to dst and returns the sha256 and sha512 of the content.
func (in *Installer) download(ctx context.Context, raw, dst string, progress func(done, total int64)) (sum256, sum512 string, err error) {
	resp, err := in.get(ctx, raw)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	h256, h512 := sha256.New(), sha512.New()
	total := resp.ContentLength
	w := io.MultiWriter(f, h256, h512)
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxDownloadBytes {
				return "", "", fmt.Errorf("download larger than %d bytes", maxDownloadBytes)
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return "", "", err
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", "", fmt.Errorf("download %s: %w", raw, rerr)
		}
	}
	if total > 0 && done != total {
		return "", "", fmt.Errorf("incomplete download: %d of %d bytes", done, total)
	}
	return hex.EncodeToString(h256.Sum(nil)), hex.EncodeToString(h512.Sum(nil)), f.Close()
}

// fetchSmall downloads a small file (checksum, icon) into memory.
func (in *Installer) fetchSmall(ctx context.Context, raw string) ([]byte, error) {
	resp, err := in.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSmallBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxSmallBytes {
		return nil, fmt.Errorf("%s larger than %d bytes", raw, maxSmallBytes)
	}
	return b, nil
}

var (
	reHex     = regexp.MustCompile(`^[0-9a-fA-F]{64}$|^[0-9a-fA-F]{128}$`)
	reBSDLine = regexp.MustCompile(`^SHA(256|512) \((.+)\) = ([0-9a-fA-F]+)$`)
)

// ParseChecksums reads files in sha256sum format ("hex  name",
// "hex *name"), BSD format ("SHA256 (name) = hex") or just "hex" (a .sha256
// file for a single asset). Returns the (lowercase) hash of assetName, or "".
func ParseChecksums(data []byte, assetName string) string {
	var lone []string
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := reBSDLine.FindStringSubmatch(line); m != nil {
			if baseName(m[2]) == assetName {
				return strings.ToLower(m[3])
			}
			continue
		}
		fields := strings.Fields(line)
		if !reHex.MatchString(fields[0]) {
			continue
		}
		if len(fields) == 1 {
			lone = append(lone, strings.ToLower(fields[0]))
			continue
		}
		name := strings.TrimPrefix(strings.Join(fields[1:], " "), "*")
		if baseName(name) == assetName {
			return strings.ToLower(fields[0])
		}
	}
	if len(lone) == 1 {
		return lone[0]
	}
	return ""
}

func baseName(p string) string {
	p = strings.TrimPrefix(p, "./")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// expected finds an asset's expected hash: first the API digest, then the
// release checksum file. Returns "" if there is none.
func (in *Installer) expected(ctx context.Context, digest, checksumURL, assetName string) (string, error) {
	if algo, hexsum, ok := strings.Cut(digest, ":"); ok && (algo == "sha256" || algo == "sha512") && reHex.MatchString(hexsum) {
		return strings.ToLower(hexsum), nil
	}
	if checksumURL == "" {
		return "", nil
	}
	data, err := in.fetchSmall(ctx, checksumURL)
	if err != nil {
		return "", fmt.Errorf("download checksum: %w", err)
	}
	sum := ParseChecksums(data, assetName)
	if sum == "" {
		return "", fmt.Errorf("%w: %s does not appear in %s", ErrChecksum, assetName, checksumURL)
	}
	return sum, nil
}

// verify compares the expected hash with the one computed while downloading.
func verify(expected, sum256, sum512 string) error {
	var got string
	switch len(expected) {
	case 64:
		got = sum256
	case 128:
		got = sum512
	default:
		return fmt.Errorf("%w: invalid expected hash", ErrChecksum)
	}
	if got != expected {
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksum, expected, got)
	}
	return nil
}
