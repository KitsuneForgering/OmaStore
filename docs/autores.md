# Publicando um app na OmaStore

A OmaStore indexa apps **standalone** para o Omarchy (não plugins nem temas)
direto do GitHub. Não há cadastro, mas há um requisito: **o repositório precisa
ter um `omastore.toml` na raiz** (seção 6). Sem ele, o repositório não entra no
catálogo, mesmo com o topic `omarchy`. O arquivo pode até estar vazio: a
presença dele é o sinal de que você quer o app na loja.

> Atalho: as skills em [`skills/`](../skills/) fazem isso com o Claude Code:
> `omastore-manifest` escreve o manifesto, `omastore-release` configura as
> releases e `omastore-check` audita tudo.

## 1. Seja encontrado

- Tenha o `omastore.toml` na raiz do branch padrão: a loja encontra
  repositórios pela busca de código do GitHub por esse arquivo, mesmo sem
  topic.

- Adicione o topic **`omarchy`** ao repositório (Settings → Topics).
- Adicione topics que descrevam o app; eles definem a categoria no catálogo:

  | Categoria | Exemplos de topics |
  |---|---|
  | Graphics | `photo`, `image`, `design`, `drawing`, `annotation` |
  | AudioVideo | `audio`, `music`, `video`, `voice`, `speech`, `recorder` |
  | Development | `developer-tools`, `editor`, `git`, `database` |
  | Network | `email`, `browser`, `chat`, `vpn`, `bluetooth` |
  | Office | `notes`, `calendar`, `pdf`, `productivity` |
  | System | `virtualization`, `monitor`, `hyprland`, `backup` |
  | Game | `game`, `emulator` |

  Sem topics, a categoria é deduzida da descrição e cai em *Utility* se nada
  casar.
- Topics `cli`, `tui` ou `terminal` fazem o atalho abrir num terminal.
- Repositórios arquivados não entram; forks não aparecem na busca por topic.

## 2. Publique uma release com binário para Linux

Só a **última release estável** (não pre-release) é considerada. Nomeie os
assets com sistema e arquitetura:

```
meuapp-1.2.0-x86_64-linux.tar.gz
meuapp-1.2.0-aarch64-linux.tar.gz
```

- Arquiteturas reconhecidas: `x86_64`/`amd64`/`x64` e `aarch64`/`arm64`.
- Formatos, em ordem de preferência:
  1. `.tar.gz`, `.tar.xz`, `.tar.zst`, `.tar.bz2`;
  2. `.zip` (precisa de `linux` ou da arquitetura no nome);
  3. binário puro (idem);
  4. `.AppImage`;
  5. `.pkg.tar.zst`.
- Ignorados: `.deb`, `.rpm`, `.dmg`, `.exe`, `.msi`, tarballs de código-fonte
  (`source`, `src`) e assets com `darwin`, `macos`, `windows` etc. no nome.

**Prefira um tarball portátil.** Dos pacotes `.pkg.tar.zst` só é extraído o
conteúdo de `usr/` (scripts `.INSTALL` nunca rodam). Como tudo fica no
`$HOME`, e não em `/usr`, programas que procuram dados em caminhos absolutos
como `/usr/share/meuapp` não funcionam.

### O executável

Dentro do pacote, o executável principal é escolhido por, nesta ordem:

1. nome igual ao do repositório;
2. estar em `bin/` ou `usr/bin/`;
3. ser ELF (não script).

Bibliotecas `.so` e arquivos em `lib/` e `share/` são ignorados. Scripts
precisam do bit de execução e de shebang.

O comando instalado em `~/.local/bin` é um pequeno lançador que chama o
caminho absoluto do seu binário, então `$0`/`argv[0]` apontam para o arquivo
real e caminhos relativos a ele (`$(dirname "$0")/../lib`) funcionam.

O nome do comando não pode colidir com um comando do sistema (`/usr/bin/...`):
a instalação é recusada para não encobrir, por exemplo, `ls` ou `sudo`.

## 3. Publique checksums

A OmaStore confere o sha256 que o próprio GitHub calcula para cada asset
(campo `digest`). Se preferir publicar os seus, qualquer um destes serve:

- `meuapp-1.2.0-x86_64-linux.tar.gz.sha256` (só o hash, ou `hash  nome`);
- `checksums.txt` / `SHA256SUMS` no formato do `sha256sum`, ou o formato BSD
  `SHA256 (nome) = hash`.

Se o hash não bater, nada é instalado. Releases sem nenhum checksum pedem
confirmação ao usuário.

## 4. Ícone e screenshots

- **Ícone:** um `icon.svg`/`icon.png` na raiz, em `assets/`, `icons/`,
  `resources/` ou `data/`. Também servem nomes como `<repo>.svg`, `logo.svg` e
  `app-icon.png`, ou a estrutura `icons/hicolor/<tam>/apps/`. SVG é o
  preferido; PNG de pelo menos 256×256 é redimensionado para o tema.
- **Screenshots:** as imagens do README (badges são ignorados) e arquivos em
  pastas ou com nomes contendo `screenshot`, `preview` ou `demo`. Até 8 são
  exibidas.

## 5. Descrição

- O resumo é a descrição do repositório (a frase curta no topo do GitHub); se
  estiver vazia, o primeiro parágrafo do README.
- O nome exibido é o do repositório, com a grafia do primeiro título do
  README quando eles batem (ex.: repo `omaphoto`, título `# OmaPhoto`).
- O README é exibido na página do app, com links relativos convertidos em
  absolutos.

## 6. O manifesto `omastore.toml` (obrigatório)

O arquivo precisa existir na raiz do branch padrão. Todos os campos são
opcionais; o que não for declarado é deduzido pelas regras acima. Declare o
que as heurísticas errariam (ícone, executável, asset).

```toml
kind = "app"                               # padrão; "plugin" e "theme" não são indexados
name = "RAWmakase"                         # nome exibido
summary = "Alternativa livre ao Lightroom" # resumo (até 300 caracteres)
categories = ["Graphics", "Photography"]   # freedesktop; a 1ª principal vira a categoria
icon = "packaging/rawmakase.svg"           # PNG ou SVG, caminho no repositório
screenshots = ["docs/images/screenshot.png", "https://exemplo.com/tela.png"]
terminal = false                           # abrir num terminal?

[linux.x86_64]                             # ou aarch64
asset = "rawmakase-{version}-x86_64-linux.tar.gz"   # {version} = tag sem "v"; {tag}; *
exec = "usr/bin/rawmakase"                 # executável dentro do pacote ({version}/{tag} valem aqui também)
```

Regras:

- **Caminhos:** precisam ser relativos e ficar dentro do repositório (ou do
  pacote, no caso de `exec`). Um `exec` que seja symlink para fora do pacote
  é recusado.
- **Asset:** o declarado tem prioridade sobre a escolha automática e é aceito
  mesmo com um nome que a heurística não entenderia. Se o padrão não casar
  com nenhum asset da release, a loja volta à escolha automática.
- **Campos inválidos:** são ignorados, com aviso; o resto do manifesto
  continua valendo. Um arquivo que não é TOML válido tira o repositório do
  catálogo, por isso valide antes de publicar.
- **Só apps:** `kind = "plugin"` ou `"theme"` (ou qualquer valor que não seja
  `app`) deixa o repositório fora da loja.

Valide antes de publicar:

```sh
omastore lint-manifest .        # no diretório do repositório
```

O modo `lint` é estrito: um campo com nome errado (ex.: `icone`) é erro. A
indexação é tolerante, para que campos de versões futuras não quebrem nada.

## Testando antes de publicar

```sh
omastore index voce/meuapp      # indexa só o seu repositório
omastore show voce/meuapp       # mostra o que foi entendido (assets, ícone, categoria)
omastore install voce/meuapp
```

Se algo foi detectado errado, declare no `omastore.toml` (seção 6) ou abra
uma issue.
