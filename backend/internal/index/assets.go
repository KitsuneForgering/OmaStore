package index

import (
	"path"
	"regexp"
	"strings"
)

// Formatos de asset suportados pelo instalador.
const (
	FormatBinary   = "binary"
	FormatTarGz    = "tar.gz"
	FormatTarXz    = "tar.xz"
	FormatTarBz2   = "tar.bz2"
	FormatTarZst   = "tar.zst"
	FormatZip      = "zip"
	FormatAppImage = "appimage"
	FormatPkg      = "pkg.tar.zst" // pacote do Arch: extraímos só o conteúdo de usr/
)

// Arquiteturas normalizadas (nomes do GOARCH).
const (
	ArchAMD64 = "amd64"
	ArchARM64 = "arm64"
)

// AssetInfo é a classificação de um asset de release.
type AssetInfo struct {
	Format string // "" = não instalável
	Arch   string // "" = não informado no nome
	// Checksum é true para arquivos de checksum (.sha256, checksums.txt...).
	Checksum bool
}

var (
	reAMD64 = regexp.MustCompile(`(?i)(^|[^a-z0-9])(x86[_-]64|amd64|x64|linux64)([^a-z0-9]|$)`)
	reARM64 = regexp.MustCompile(`(?i)(^|[^a-z0-9])(aarch64|arm64|armv8)([^a-z0-9]|$)`)
	// Arquiteturas que não suportamos.
	reOtherArch = regexp.MustCompile(`(?i)(^|[^a-z0-9])(i[3-6]86|x86[_-]32|386|armv[5-7]l?|armhf|armel|arm32|riscv64|ppc64(le)?|s390x|mips[a-z0-9]*|loong(arch)?64)([^a-z0-9]|$)`)
	// Sistemas que não são Linux.
	reOtherOS = regexp.MustCompile(`(?i)(^|[^a-z0-9])(darwin|macos|osx|apple|windows|win32|win64|freebsd|openbsd|netbsd|android|ios)([^a-z0-9]|$)`)
	// Imagens de boot/VM e firmware: não são apps.
	reBootImage = regexp.MustCompile(`(?i)(^|[^a-z0-9])(vmlinu[xz]|initrd|initramfs|bzimage|firmware|ovmf)([^a-z0-9]|$)`)
	// Tarballs de código-fonte e afins.
	reSource = regexp.MustCompile(`(?i)(^|[^a-z0-9])(source|sources|src|recipe|vendor|debug|dbgsym|headers|devel)([^a-z0-9]|$)`)
	// Checksums publicados junto com a release.
	reChecksum = regexp.MustCompile(`(?i)(\.(sha256|sha512|sha256sum|sha512sum|md5)$|^(sha256sums|sha512sums|checksums?)(\.txt)?$|checksums?\.txt$)`)
)

// Extensões de arquivos que nunca são instaláveis.
var skipExt = []string{
	".deb", ".rpm", ".dmg", ".pkg", ".exe", ".msi", ".apk", ".snap", ".flatpak", ".flatpakref",
	".sig", ".asc", ".pem", ".crt", ".sbom", ".spdx", ".json", ".yml", ".yaml", ".txt", ".md",
	".sha1", ".blockmap", ".vsix", ".whl", ".jar", ".nupkg", ".7z", ".rar", ".dll", ".so", ".dylib",
	".intoto.jsonl", ".bundle", ".zsync", ".iso", ".img", ".qcow2", ".vhd", ".vhdx", ".vmdk", ".efi",
}

// ClassifyAsset identifica formato e arquitetura de um asset pelo nome.
func ClassifyAsset(name string) AssetInfo {
	lower := strings.ToLower(name)
	if reChecksum.MatchString(lower) {
		return AssetInfo{Checksum: true}
	}
	var info AssetInfo
	switch {
	case reAMD64.MatchString(lower):
		info.Arch = ArchAMD64
	case reARM64.MatchString(lower):
		info.Arch = ArchARM64
	case reOtherArch.MatchString(lower):
		return AssetInfo{}
	}
	if reOtherOS.MatchString(lower) || reSource.MatchString(lower) || reBootImage.MatchString(lower) {
		return AssetInfo{}
	}
	switch {
	case strings.HasSuffix(lower, ".pkg.tar.zst"):
		info.Format = FormatPkg
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		info.Format = FormatTarGz
	case strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".txz"):
		info.Format = FormatTarXz
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		info.Format = FormatTarBz2
	case strings.HasSuffix(lower, ".tar.zst"):
		info.Format = FormatTarZst
	case strings.HasSuffix(lower, ".zip"):
		// Zips sem arquitetura nem "linux" no nome costumam ser de Windows.
		if info.Arch == "" && !strings.Contains(lower, "linux") {
			return AssetInfo{}
		}
		info.Format = FormatZip
	case strings.HasSuffix(lower, ".appimage"):
		info.Format = FormatAppImage
	default:
		for _, e := range skipExt {
			if strings.HasSuffix(lower, e) {
				return AssetInfo{}
			}
		}
		// Binário puro: sem extensão reconhecida. Exigimos um indício de que é
		// para Linux (arquitetura ou "linux" no nome) para não pegar lixo.
		ext := path.Ext(lower)
		if ext != "" && !isVersionLike(ext) && !strings.Contains(ext, "linux") && info.Arch == "" {
			return AssetInfo{}
		}
		if info.Arch == "" && !strings.Contains(lower, "linux") {
			return AssetInfo{}
		}
		info.Format = FormatBinary
	}
	return info
}

// FormatOf identifica o formato só pela extensão, sem as regras de sistema
// e arquitetura de ClassifyAsset (usado para assets declarados no
// manifesto). Retorna "" para formatos que o instalador não suporta.
func FormatOf(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".pkg.tar.zst"):
		return FormatPkg
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return FormatTarGz
	case strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".txz"):
		return FormatTarXz
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		return FormatTarBz2
	case strings.HasSuffix(lower, ".tar.zst"):
		return FormatTarZst
	case strings.HasSuffix(lower, ".zip"):
		return FormatZip
	case strings.HasSuffix(lower, ".appimage"):
		return FormatAppImage
	}
	for _, e := range skipExt {
		if strings.HasSuffix(lower, e) {
			return ""
		}
	}
	return FormatBinary
}

var reVersionExt = regexp.MustCompile(`^\.[0-9]+(-.*)?$`)

// isVersionLike diz se ext é na verdade parte de uma versão ("app-1.2.3").
func isVersionLike(ext string) bool { return reVersionExt.MatchString(ext) }

// Installable diz se o asset pode ser instalado nesta arquitetura.
// Um asset sem arquitetura no nome é aceito como candidato genérico.
func (a AssetInfo) Installable(goarch string) bool {
	return a.Format != "" && (a.Arch == "" || a.Arch == goarch)
}

// formatRank ordena os formatos por preferência na escolha do asset.
var formatRank = map[string]int{
	FormatTarGz: 0, FormatTarXz: 0, FormatTarZst: 0, FormatTarBz2: 0, FormatZip: 1,
	FormatBinary: 2, FormatAppImage: 3, FormatPkg: 4,
}

// FormatRank retorna a preferência do formato (menor é melhor).
func FormatRank(format string) int {
	if r, ok := formatRank[format]; ok {
		return r
	}
	return 99
}
