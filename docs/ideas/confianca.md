# Confiança: attestations, reputação e sandbox

## Evidência

- A API do GitHub já devolve `digest` (sha256) por asset, e o instalador o
  confere. Mas o digest só prova que o download não foi corrompido: não diz
  **quem** construiu o binário.
- Alguns apps publicam assinaturas (`checksums.txt.sig` no RAWmakase), que
  hoje só descartamos.
- Instalar software de qualquer repositório com o topic `omarchy` é, na
  prática, confiar em qualquer pessoa que marque esse topic.

## Proposta

1. **GitHub artifact attestations:** releases geradas por GitHub Actions com
   `actions/attest-build-provenance` têm proveniência SLSA verificável pela
   API (`GET /repos/{owner}/{repo}/attestations/{digest}`). Mostrar
   "construído pelo workflow X do próprio repositório" quando disponível, e
   permitir exigir isso numa configuração ("somente apps com proveniência").
2. **Assinaturas minisign/cosign:** verificar quando o repositório publicar
   a chave pública (ou no manifesto, ver [manifesto.md](manifesto.md)).
3. **Sinais de reputação no catálogo:** idade do repositório, número de
   releases, se a release é mais nova que o último commit, e se o asset foi
   enviado por um bot de CI (`uploader.type`). Exibir, sem bloquear.
4. **Aviso explícito sem checksum:** hoje só registramos um warning no log.
   O frontend deveria pedir confirmação.
5. **Sandbox opcional:** gerar o `Exec` do `.desktop` via `bwrap` com perfil
   restrito (sem `$HOME` inteiro), para quem quiser. Não pode ser o padrão,
   porque muitos apps precisam do `$HOME`.

## Custo

Attestations e o aviso sem checksum são pequenos; minisign é médio (formato
simples, há implementação em Go); o sandbox é grande (perfis por app).

## Riscos

- Exigir proveniência por padrão tiraria do catálogo a maioria dos apps atuais.
- O sandbox quebra apps de formas difíceis de diagnosticar; precisa ser opt-in
  por app.
