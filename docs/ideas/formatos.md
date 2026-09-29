# Package formats and relocation into `$HOME`

## Evidence

- `ZacharyZhang-NY/OmaPhoto` publishes only `.deb`, `.rpm` and `.pkg.tar.zst`;
  `pch/rawmakase` published `.pkg.tar.zst`/`.rpm` up to v0.1.5 and only then
  started publishing a tarball.
- From `.pkg.tar.zst` we extract only `usr/` and discard `.INSTALL`. This is
  safe, but the app ends up living in
  `~/.local/share/omastore/apps/<repo>/<version>/usr/...`. Programs that
  look for data at absolute paths (`/usr/share/<app>`) break.
  RAWmakase works because its launcher uses paths relative to `$0`.

## Proposal

1. **`.deb`:** `ar` format with `data.tar.{gz,xz,zst}`; extract only `usr/`,
   as with `.pkg.tar.zst`, and ignore the `control` scripts. Small cost.
2. **`.rpm`:** its own header plus a compressed `cpio` payload. Medium cost;
   only worth it if an app shows up that publishes only `.rpm`.
3. **Detect absolute paths:** after extracting a package, look in the
   ELF binaries (`.rodata` section, via `debug/elf`) for strings such as `/usr/share/<name>`
   or `/usr/lib/<name>` that do not exist on the system. If there are any, mark the
   installation as "may not work outside /usr" and warn in the interface.
   It only reads the file; nothing is executed.
4. **Prefer portable tarballs** (which `SelectAsset` already does) and
   document it in the authors guide.

## Risks

- Extracting a `.deb` from another distribution may bring library dependencies
  that do not exist on Arch; the warning from item 3 helps, but does not solve it.
