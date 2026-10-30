package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luisdvss/docty/internal/ingest"
	"github.com/luisdvss/docty/internal/source"
	_ "modernc.org/sqlite"
)

// Index is a read handle over the SQLite index.
type Index struct {
	db   *sql.DB
	path string
}

// Open opens an existing index.
func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	var version string
	err = db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&version)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("índice inválido: %w", err)
	}
	if version != schemaVersion {
		db.Close()
		return nil, fmt.Errorf("índice con esquema %s, se esperaba %s: reconstruye con docty update <lang>", version, schemaVersion)
	}
	return &Index{db: db, path: path}, nil
}

// Close releases the database handle.
func (ix *Index) Close() error { return ix.db.Close() }

// Path returns the file backing the index.
func (ix *Index) Path() string { return ix.path }

// Build creates a fresh index in memory-safe fashion: writes to a temporary
// file, closes it and atomically replaces the previous one.
func Build(ctx context.Context, path string, m source.Manifest, version string, docs []*ingest.Document) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	_ = os.Remove(tmp + "-journal")

	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return err
	}
	// DELETE journal: the file is renamed at the end, so there must be no
	// dangling -wal/-shm files next to it.
	if _, err := db.Exec(`PRAGMA journal_mode = DELETE`); err != nil {
		db.Close()
		return err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return err
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return fmt.Errorf("creando esquema: %w", err)
		}
	}

	if err := insertAll(ctx, db, m, version, docs); err != nil {
		db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

func insertAll(ctx context.Context, db *sql.DB, m source.Manifest, version string, docs []*ingest.Document) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	checksum := checksumDocs(docs)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sources (id, name, provider, version, fetched_at, page_count, checksum)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Name, m.Provider, version, now, len(docs), checksum); err != nil {
		return err
	}

	for _, d := range docs {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO documents
			 (source_id, symbol, symbol_norm, kind, title, slug, url, summary_md,
			  signature, content_hash)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, d.Symbol, strings.ToLower(d.Symbol), d.Kind, d.Title, d.Slug, d.URL,
			d.Summary, signatureOf(d), contentHash(d))
		if err != nil {
			return fmt.Errorf("insertando %s: %w", d.Symbol, err)
		}
		docID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for _, s := range d.Sections {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO sections (document_id, symbol, ord, heading, heading_path, body_md, token_count)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				docID, d.Symbol, s.Ord, s.Heading, s.HeadingPath, s.Body, s.Tokens); err != nil {
				return fmt.Errorf("insertando sección %q de %s: %w", s.Heading, d.Symbol, err)
			}
		}
	}

	for k, v := range map[string]string{
		"schema_version": schemaVersion,
		"source_version": version,
		"built_at":       now,
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)`, k, v); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO sections_fts(sections_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("reconstruyendo FTS: %w", err)
	}
	return tx.Commit()
}

func contentHash(d *ingest.Document) string {
	h := sha256.New()
	h.Write([]byte(d.Symbol))
	h.Write([]byte(d.Summary))
	for _, s := range d.Sections {
		h.Write([]byte(s.Heading))
		h.Write([]byte(s.Body))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func checksumDocs(docs []*ingest.Document) string {
	h := sha256.New()
	for _, d := range docs {
		h.Write([]byte(d.Symbol))
		h.Write([]byte(contentHash(d)))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// signatureOf extracts a rough signature from the Syntax section: the first
// line of its first fenced code block.
func signatureOf(d *ingest.Document) string {
	for _, s := range d.Sections {
		if !strings.EqualFold(s.Heading, "Syntax") {
			continue
		}
		inFence := false
		for _, line := range strings.Split(s.Body, "\n") {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "```") {
				if inFence {
					break
				}
				inFence = true
				continue
			}
			if inFence && t != "" {
				return t
			}
		}
	}
	return ""
}
