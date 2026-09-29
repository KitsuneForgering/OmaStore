# Minimum relevance in search

## Evidence

With the real catalog (25 installable apps), the query `notes` returns
OMARCHIST, wayscriber and RAWmakase. None of them is a notes app: they all
match only because the word "note(s)" appears in the README, which has a low
weight. The user gets results that look random instead of "nothing found".

## Proposal

1. Mark in each result **where** the term matched (name, topics, summary or
   only the README) and expose it in `AppItem` (`matchedIn`).
2. If no result matched outside the README, the interface shows "No notes app
   in the catalog; results that only mention *notes* in their
   documentation:" and lists them separately.
3. Do not use an absolute score threshold: it depends on the catalog size and
   would break consistency across different catalogs. The rule "matched in a
   strong field or not" is deterministic and independent of the corpus.

## Cost

Small: `search.Index` already knows the per-field weights; it only needs to keep
per-field scores, not just the weighted sum.
