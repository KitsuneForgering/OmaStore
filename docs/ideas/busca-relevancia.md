# Relevância mínima na busca

## Evidência

Com o catálogo real (25 apps instaláveis), a consulta `notes` devolve
OMARCHIST, wayscriber e RAWmakase. Nenhum deles é um app de notas: todos
casam só porque a palavra "note(s)" aparece no README, que tem peso baixo. O
usuário recebe resultados que parecem aleatórios em vez de "nada encontrado".

## Proposta

1. Marcar em cada resultado **onde** o termo casou (nome, topics, resumo ou
   só README) e expor isso no `AppItem` (`matchedIn`).
2. Se nenhum resultado casou fora do README, a interface mostra "Nenhum app
   de notas no catálogo; resultados que só mencionam *notes* na
   documentação:" e os lista separados.
3. Não usar limiar absoluto de score: ele depende do tamanho do catálogo e
   quebraria a consistência entre catálogos diferentes. A regra "casou em
   campo forte ou não" é determinística e independe do corpus.

## Custo

Pequeno: o `search.Index` já sabe os pesos por campo; falta guardar por
campo, e não só a soma ponderada.
