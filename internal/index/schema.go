package index

// schemaVersion is stored in the meta table and checked when opening an index.
const schemaVersion = "2"

var schema = []string{
	`CREATE TABLE meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	`CREATE TABLE sources (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		provider    TEXT NOT NULL,
		version     TEXT NOT NULL,
		fetched_at  TEXT NOT NULL,
		page_count  INTEGER NOT NULL,
		checksum    TEXT NOT NULL
	)`,
	`CREATE TABLE documents (
		id           INTEGER PRIMARY KEY,
		source_id    TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
		symbol       TEXT NOT NULL,
		symbol_norm  TEXT NOT NULL,
		kind         TEXT NOT NULL,
		title        TEXT NOT NULL,
		slug         TEXT NOT NULL,
		url          TEXT NOT NULL,
		summary_md   TEXT NOT NULL,
		signature    TEXT,
		since        TEXT,
		deprecated   INTEGER NOT NULL DEFAULT 0,
		experimental INTEGER NOT NULL DEFAULT 0,
		content_hash TEXT NOT NULL,
		UNIQUE (source_id, slug)
	)`,
	`CREATE INDEX idx_documents_symbol ON documents(source_id, symbol_norm)`,
	`CREATE TABLE sections (
		id           INTEGER PRIMARY KEY,
		document_id  INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
		symbol       TEXT NOT NULL,
		ord          INTEGER NOT NULL,
		heading      TEXT NOT NULL,
		heading_path TEXT NOT NULL,
		body_md      TEXT NOT NULL,
		token_count  INTEGER NOT NULL
	)`,
	`CREATE INDEX idx_sections_doc ON sections(document_id, ord)`,
	`CREATE TABLE examples (
		id          INTEGER PRIMARY KEY,
		document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
		section_id  INTEGER REFERENCES sections(id) ON DELETE CASCADE,
		ord         INTEGER NOT NULL,
		lang        TEXT,
		code        TEXT NOT NULL,
		caption     TEXT
	)`,
	`CREATE VIRTUAL TABLE sections_fts USING fts5(
		heading, heading_path, body_md, symbol,
		content='sections', content_rowid='id',
		tokenize='porter unicode61 remove_diacritics 2',
		prefix='2 3'
	)`,
}
