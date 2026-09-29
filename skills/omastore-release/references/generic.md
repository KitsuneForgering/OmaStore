# Generic release

For any build that produces an executable (Zig, Nim, Crystal, a script with a
shebang packaged together with its data, or an AppImage already produced by
another tool), the rule is the same: one file per architecture named
`<app>-<version>-<arch>-linux.<ext>`.

## Tarball from a ready directory

```yaml
name: Release

on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    env:
      VERSION: ${{ github.ref_name }}
      APP: myapp
    steps:
      - uses: actions/checkout@v4
      # Replace with your project's build; the result must be an
      # executable at out/myapp.
      - run: ./build.sh
      - name: Package
        run: |
          ver="${VERSION#v}"
          dir="$APP-$ver-x86_64-linux"
          mkdir -p "dist/$dir/bin"
          install -m755 "out/$APP" "dist/$dir/bin/$APP"
          tar -C dist -czf "dist/$dir.tar.gz" "$dir"
          (cd dist && sha256sum ./*.tar.gz > checksums.txt)
      - uses: softprops/action-gh-release@v2
        with:
          generate_release_notes: true
          files: |
            dist/*.tar.gz
            dist/checksums.txt
```

## AppImage

OmaStore installs AppImages, but they come last in the preference order (after
tarballs, zip and plain binary). Expected name:
`MyApp-1.2.0-x86_64.AppImage`. Declare it in the manifest if there is another
Linux file in the same release:

```toml
[linux.x86_64]
asset = "MyApp-{version}-x86_64.AppImage"
```

## Scripts

A Python/Node/Bash app can be distributed as a tarball with a launcher
`bin/myapp` (shebang + executable bit) and the code in `share/myapp/`. The
launcher must locate the code relative to itself:

```sh
#!/bin/sh
here=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec python3 "$here/share/myapp/main.py" "$@"
```

OmaStore creates a launcher in `~/.local/bin` that calls your executable's
real path, so `$0` always points to the file inside the package.
Runtime dependencies (Python, modules) must be documented in the
README: the store does not install dependencies.
