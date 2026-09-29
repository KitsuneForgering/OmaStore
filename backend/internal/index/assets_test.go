package index

import "testing"

func TestClassifyAsset(t *testing.T) {
	cases := []struct {
		name string
		want AssetInfo
	}{
		{"omadesign-0.6.0-x86_64-unknown-linux-gnu.tar.gz", AssetInfo{Format: FormatTarGz, Arch: ArchAMD64}},
		{"omadesign-0.6.0-aarch64-unknown-linux-gnu.tar.gz", AssetInfo{Format: FormatTarGz, Arch: ArchARM64}},
		{"omadesign-0.6.0-x86_64-unknown-linux-gnu.tar.gz.sha256", AssetInfo{Checksum: true}},
		{"checksums.txt", AssetInfo{Checksum: true}},
		{"SHA256SUMS", AssetInfo{Checksum: true}},
		{"checksums.txt.sig", AssetInfo{}},
		{"rawmakase-0.1.5-1-x86_64.pkg.tar.zst", AssetInfo{Format: FormatPkg, Arch: ArchAMD64}},
		{"rawmakase-0.1.5-1.x86_64.rpm", AssetInfo{}},
		{"omaphoto-noble_amd64.deb", AssetInfo{}},
		{"rawmakase-0.1.5-source.tar.gz", AssetInfo{}},
		{"rawmakase-0.1.5-arch-recipe.tar.gz", AssetInfo{}},
		{"app_Linux_x86_64.tar.gz", AssetInfo{Format: FormatTarGz, Arch: ArchAMD64}},
		{"app_Darwin_arm64.tar.gz", AssetInfo{}},
		{"app-windows-amd64.zip", AssetInfo{}},
		{"app-linux-amd64", AssetInfo{Format: FormatBinary, Arch: ArchAMD64}},
		{"app-linux-arm64", AssetInfo{Format: FormatBinary, Arch: ArchARM64}},
		{"app-linux-armv7", AssetInfo{}},
		{"app-linux-386", AssetInfo{}},
		{"app-1.2.3-linux", AssetInfo{Format: FormatBinary}},
		{"app", AssetInfo{}},
		{"App-1.0-x86_64.AppImage", AssetInfo{Format: FormatAppImage, Arch: ArchAMD64}},
		{"app-linux.zip", AssetInfo{Format: FormatZip}},
		{"app.tar.xz", AssetInfo{Format: FormatTarXz}},
		{"app-x86_64.sbom.json", AssetInfo{}},
		{"app_1.0_linux_amd64.tar.zst", AssetInfo{Format: FormatTarZst, Arch: ArchAMD64}},
		{"vmlinuz-linux", AssetInfo{}},
		{"initramfs-linux.img", AssetInfo{}},
		{"winq-emu-alpha10-portable.zip", AssetInfo{}},
		{"app-x86_64.zip", AssetInfo{Format: FormatZip, Arch: ArchAMD64}},
		{"archlinux-x86_64.iso", AssetInfo{}},
	}
	for _, c := range cases {
		if got := ClassifyAsset(c.name); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestInstallable(t *testing.T) {
	if !(AssetInfo{Format: FormatZip}).Installable("arm64") {
		t.Error("a generic asset should do")
	}
	if (AssetInfo{Format: FormatZip, Arch: ArchAMD64}).Installable("arm64") {
		t.Error("amd64 does not work on arm64")
	}
	if (AssetInfo{Checksum: true}).Installable("amd64") {
		t.Error("a checksum is not installable")
	}
}
