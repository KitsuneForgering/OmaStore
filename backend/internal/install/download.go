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

// Limites de download.
var (
	maxDownloadBytes int64 = 2 << 30
	maxSmallBytes    int64 = 5 << 20 // checksums e ícones
)

// ErrChecksum indica que o arquivo baixado não bate com o checksum publicado.
var ErrChecksum = errors.New("checksum não confere")

func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL inválida %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL %q não é https", raw)
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
	resp, err := in.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("baixar %s: %w", raw, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("baixar %s: HTTP %d", raw, resp.StatusCode)
	}
	return resp, nil
}

// download grava raw em dst e retorna o sha256 e o sha512 do conteúdo.
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
				return "", "", fmt.Errorf("download maior que %d bytes", maxDownloadBytes)
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
			return "", "", fmt.Errorf("baixar %s: %w", raw, rerr)
		}
	}
	if total > 0 && done != total {
		return "", "", fmt.Errorf("download incompleto: %d de %d bytes", done, total)
	}
	return hex.EncodeToString(h256.Sum(nil)), hex.EncodeToString(h512.Sum(nil)), f.Close()
}

// fetchSmall baixa um arquivo pequeno (checksum, ícone) para a memória.
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
		return nil, fmt.Errorf("%s maior que %d bytes", raw, maxSmallBytes)
	}
	return b, nil
}

var (
	reHex     = regexp.MustCompile(`^[0-9a-fA-F]{64}$|^[0-9a-fA-F]{128}$`)
	reBSDLine = regexp.MustCompile(`^SHA(256|512) \((.+)\) = ([0-9a-fA-F]+)$`)
)

// ParseChecksums lê arquivos no formato do sha256sum ("hex  nome",
// "hex *nome"), BSD ("SHA256 (nome) = hex") ou só "hex" (arquivo .sha256 de
// um único asset). Retorna o hash (minúsculo) de assetName, ou "".
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

// expected descobre o hash esperado de um asset: primeiro o digest da API,
// depois o arquivo de checksum da release. Retorna "" se não houver nenhum.
func (in *Installer) expected(ctx context.Context, digest, checksumURL, assetName string) (string, error) {
	if algo, hexsum, ok := strings.Cut(digest, ":"); ok && (algo == "sha256" || algo == "sha512") && reHex.MatchString(hexsum) {
		return strings.ToLower(hexsum), nil
	}
	if checksumURL == "" {
		return "", nil
	}
	data, err := in.fetchSmall(ctx, checksumURL)
	if err != nil {
		return "", fmt.Errorf("baixar checksum: %w", err)
	}
	sum := ParseChecksums(data, assetName)
	if sum == "" {
		return "", fmt.Errorf("%w: %s não aparece em %s", ErrChecksum, assetName, checksumURL)
	}
	return sum, nil
}

// verify compara o hash esperado com o calculado no download.
func verify(expected, sum256, sum512 string) error {
	var got string
	switch len(expected) {
	case 64:
		got = sum256
	case 128:
		got = sum512
	default:
		return fmt.Errorf("%w: hash esperado inválido", ErrChecksum)
	}
	if got != expected {
		return fmt.Errorf("%w: esperado %s, obtido %s", ErrChecksum, expected, got)
	}
	return nil
}
