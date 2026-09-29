# Daemon sob demanda, índice periódico e aviso de atualizações

> **Status:** implementado na Fase 13: `-idle-timeout` no `omastored`,
> `omastore update --check --notify` (`internal/notify`, D-Bus direto) e
> `packaging/systemd/omastore-index.{service,timer}`.

## Evidência

- Com a socket activation (`packaging/systemd/omastored.socket`), o systemd já
  inicia o daemon na primeira conexão, mas ele fica rodando para sempre.
- Hoje a indexação só acontece quando alguém pede (`index.start` ou
  `omastore index`). Um usuário que não abre a loja nunca fica sabendo de
  atualizações dos apps instalados.

## Proposta

1. **Encerrar quando ocioso:** sem conexões e sem jobs por N minutos (padrão
   10), o daemon encerra limpo. Com socket activation, a próxima conexão o
   inicia de novo. Só ligar isso quando o socket vier do systemd
   (`ActivationListener() != nil`).
2. **Timer do systemd:** `omastore-index.timer` (diário, `Persistent=true`,
   `RandomizedDelaySec=1h`) roda `omastore index` e, em seguida,
   `omastore update --check`.
3. **`update --check`:** lista os apps com versão nova sem instalar e, se
   houver, manda uma notificação desktop (`notify-send` via
   `org.freedesktop.Notifications` no D-Bus, sem chamar binários pelo `PATH`).
   Atualizar sozinho continua opcional (configuração).

## Custo

Pequeno: um contador de atividade no `Server`, as unidades do timer, a flag
`--check` na CLI e a notificação por D-Bus (`godbus/dbus`).

## Riscos

- Atualizar automaticamente sem o usuário ver é arriscado (binário novo não
  revisado); por isso o padrão é só notificar.
- Encerrar ocioso com jobs longos: o contador precisa considerar jobs em
  andamento, e não só conexões.
