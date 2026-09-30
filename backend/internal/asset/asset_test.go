package asset

import "testing"

func TestClassifyAsset(t *testing.T) {
	cases := []struct {
		name string
		want Info
	}{
		{"omadesign-0.6.0-x86_64-unknown-linux-gnu.tar.gz", Info{Format: FormatTarGz, Arch: ArchAMD64}},
		{"omadesign-0.6.0-aarch64-unknown-linux-gnu.tar.gz", Info{Format: FormatTarGz, Arch: ArchARM64}},
		{"omadesign-0.6.0-x86_64-unknown-linux-gnu.tar.gz.sha256", Info{Checksum: true}},
		{"checksums.txt", Info{Checksum: true}},
		{"SHA256SUMS", Info{Checksum: true}},
		{"checksums.txt.sig", Info{}},
		{"rawmakase-0.1.5-1-x86_64.pkg.tar.zst", Info{Format: FormatPkg, Arch: ArchAMD64}},
		{"rawmakase-0.1.5-1.x86_64.rpm", Info{}},
		{"omaphoto-noble_amd64.deb", Info{}},
		{"rawmakase-0.1.5-source.tar.gz", Info{}},
		{"rawmakase-0.1.5-arch-recipe.tar.gz", Info{}},
		{"app_Linux_x86_64.tar.gz", Info{Format: FormatTarGz, Arch: ArchAMD64}},
		{"app_Darwin_arm64.tar.gz", Info{}},
		{"app-windows-amd64.zip", Info{}},
		{"app-linux-amd64", Info{Format: FormatBinary, Arch: ArchAMD64}},
		{"app-linux-arm64", Info{Format: FormatBinary, Arch: ArchARM64}},
		{"app-linux-armv7", Info{}},
		{"app-linux-386", Info{}},
		{"app-1.2.3-linux", Info{Format: FormatBinary}},
		{"app", Info{}},
		{"App-1.0-x86_64.AppImage", Info{Format: FormatAppImage, Arch: ArchAMD64}},
		{"app-linux.zip", Info{Format: FormatZip}},
		{"app.tar.xz", Info{Format: FormatTarXz}},
		{"app-x86_64.sbom.json", Info{}},
		{"app_1.0_linux_amd64.tar.zst", Info{Format: FormatTarZst, Arch: ArchAMD64}},
		{"vmlinuz-linux", Info{}},
		{"initramfs-linux.img", Info{}},
		{"winq-emu-alpha10-portable.zip", Info{}},
		{"app-x86_64.zip", Info{Format: FormatZip, Arch: ArchAMD64}},
		{"archlinux-x86_64.iso", Info{}},
		// Signatures, bare tarballs and single-file compression are not binaries.
		{"app-linux-amd64.minisig", Info{}},
		{"app-linux-amd64.sigstore", Info{}},
		{"app-linux-amd64.tar", Info{}},
		{"app-linux-amd64.gz", Info{}},
		{"app-linux-amd64.xz", Info{}},
		{"app-linux-amd64.zst", Info{}},
		{"app-linux-amd64.bz2", Info{}},
		{"install-linux-amd64.sh", Info{}},
		{"app-v1.2-linux-amd64", Info{Format: FormatBinary, Arch: ArchAMD64}},
		{"app-x86_64.bin", Info{Format: FormatBinary, Arch: ArchAMD64}},
	}
	for _, c := range cases {
		if got := Classify(c.name); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestInstallable(t *testing.T) {
	if !(Info{Format: FormatZip}).Installable("arm64") {
		t.Error("a generic asset should do")
	}
	if (Info{Format: FormatZip, Arch: ArchAMD64}).Installable("arm64") {
		t.Error("amd64 does not work on arm64")
	}
	if (Info{Checksum: true}).Installable("amd64") {
		t.Error("a checksum is not installable")
	}
}
