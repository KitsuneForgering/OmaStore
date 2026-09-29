-- Versão da lógica do indexador que processou cada repositório. Quando as
-- regras de extração/classificação mudam, o indexador reprocessa os repos
-- gravados com versão menor mesmo que nada tenha mudado no GitHub.
ALTER TABLE repos ADD COLUMN index_version INTEGER NOT NULL DEFAULT 0;
