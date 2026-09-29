# Formatos de pacote e realocação para o `$HOME`

## Evidência

- `ZacharyZhang-NY/OmaPhoto` publica só `.deb`, `.rpm` e `.pkg.tar.zst`;
  `pch/rawmakase` até a v0.1.5 publicava `.pkg.tar.zst`/`.rpm` e só depois
  passou a publicar um tarball.
- Extraímos de `.pkg.tar.zst` apenas `usr/` e descartamos `.INSTALL`. Isso é
  seguro, mas o app passa a morar em
  `~/.local/share/omastore/apps/<repo>/<versão>/usr/...`. Programas que
  procuram dados em caminhos absolutos (`/usr/share/<app>`) quebram. O
  RAWmakase funciona porque o lançador dele usa caminhos relativos a `$0`.

## Proposta

1. **`.deb`:** formato `ar` com `data.tar.{gz,xz,zst}`; extrair só `usr/`,
   como no `.pkg.tar.zst`, e ignorar os scripts `control`. Custo pequeno.
2. **`.rpm`:** cabeçalho próprio mais payload `cpio` comprimido. Custo médio;
   só vale se aparecer app que publique apenas `.rpm`.
3. **Detectar caminhos absolutos:** depois de extrair um pacote, procurar nos
   binários ELF (seção `.rodata`, via `debug/elf`) strings como `/usr/share/<nome>`
   ou `/usr/lib/<nome>` que não existam no sistema. Se houver, marcar a
   instalação como "pode não funcionar fora de /usr" e avisar na interface.
   É só leitura do arquivo; nada é executado.
4. **Preferir tarballs portáteis** (já é o que o `SelectAsset` faz) e
   documentar isso no guia para autores.

## Riscos

- Extrair `.deb` de outra distribuição pode trazer dependências de bibliotecas
  que não existem no Arch; o aviso do item 3 ajuda, mas não resolve.
