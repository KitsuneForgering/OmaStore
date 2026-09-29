# Ideias de interface (Fase 8)

Notas para o frontend. Itens marcados como **feito** já estão na Fase 8.

- **Teclado primeiro** (*parcial: `/`, `Esc`, `Ctrl+R` e navegação no grid feitos; `i`/`u` faltam*): usuários do Omarchy vivem no teclado. `/` foca a
  busca, `j/k` navega pelo grid, `Enter` abre o detalhe, `i` instala, `u`
  atualiza, `Esc` volta.
- **Selo de verificação** (*feito*): o `AssetInfo.verified` já existe no protocolo.
  Mostrar "checksum verificado" ou "sem checksum" antes de instalar, e pedir
  confirmação no segundo caso (ver [confianca.md](confianca.md)).
- **Notas da release:** mostrar o changelog da versão nova no botão
  "Atualizar". *Backend:* guardar `body` da release no índice (hoje não é
  guardado).
- **Abrir depois de instalar:** botão "Abrir" que usa o `.desktop` gerado
  (`gtk-launch omastore-owner-repo`). A instalação continua sem executar nada;
  abrir é uma ação explícita do usuário.
- **Estado vazio** (*feito*): na primeira execução o catálogo está vazio. Iniciar
  `index.start` automaticamente e mostrar o progresso (`job.progress` traz o
  repo atual em `message`).
- **Rate limit** (*feito*): o erro `-32005` deveria sugerir `gh auth login` e dizer
  quando o limite volta.
- **Tema** (*feito*): cores de `~/.local/state/omarchy/current/theme/colors.toml`,
  recarregadas ao trocar de tema (o Omarchy substitui o diretório inteiro).
- **Instalar pelo terminal com um link:** registrar um handler `x-scheme-handler/omastore`
  para links `omastore://owner/repo` (ex.: num README), que abre o
  `omastore-gui --open owner/repo`. A opção `--open` já existe.
- **Paginação:** o `catalog.list` já aceita `limit`/`offset`; com centenas de
  apps, carregar sob demanda no scroll do GridView.
- **Tradução:** todas as strings da interface já passam por `qsTr()` e estão
  em português. Gerar `.ts` com `qt_add_translations` e um `en` para usuários
  que não falam português; a CLI e as mensagens de erro do daemon também
  estão em português e precisariam de outro mecanismo (códigos de erro já são
  estáveis, então o frontend pode traduzir pela tabela de `docs/ipc.md`).
