package store

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type Collection struct {
	ID        int64
	Name      string
	Type      string // "markdown", "claude", "codex"
	Path      string
	Pattern   string
	CreatedAt string
	UpdatedAt string
}

type Document struct {
	ID           int64
	CollectionID int64
	Path         string
	Title        string
	ContentHash  string
	Mtime        float64
	LineCount    int
	CreatedAt    string
	UpdatedAt    string
}

type Chunk struct {
	ID         int64
	DocumentID int64
	Seq        int
	Content    string
	Embedding  []float32
	ChunkType  int    // 0 = text, 1 = image
	ImagePath  string // path to image file on disk (for image chunks)
	CreatedAt  string
}

type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Path       string
	Collection string
	Content    string
	Score      float64
	ChunkType  int    // 0 = text, 1 = image
	ImagePath  string // non-empty for image chunks
}

func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	// Verify FTS5 is available (requires build tag: -tags "fts5")
	var fts5ok int
	if err := s.db.QueryRow(`SELECT 1 FROM pragma_compile_options WHERE compile_options = 'ENABLE_FTS5'`).Scan(&fts5ok); err != nil {
		return fmt.Errorf("SQLite FTS5 not enabled. Build with: make build (or: go build -tags \"fts5\")")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			type TEXT NOT NULL,
			path TEXT NOT NULL,
			pattern TEXT DEFAULT '**/*.md',
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS documents (
			id INTEGER PRIMARY KEY,
			collection_id INTEGER REFERENCES collections(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			title TEXT,
			content_hash TEXT,
			mtime REAL,
			line_count INTEGER,
			created_at TEXT,
			updated_at TEXT,
			UNIQUE(collection_id, path)
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			title, content,
			content_rowid='id',
			tokenize='unicode61'
		)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			id INTEGER PRIMARY KEY,
			document_id INTEGER REFERENCES documents(id) ON DELETE CASCADE,
			seq INTEGER,
			content TEXT,
			embedding BLOB,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_document ON chunks(document_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}

	// Add new columns for multimodal support (backward compat via ALTER TABLE)
	alterStmts := []string{
		`ALTER TABLE chunks ADD COLUMN chunk_type INTEGER DEFAULT 0`,
		`ALTER TABLE chunks ADD COLUMN image_path TEXT`,
	}
	for _, stmt := range alterStmts {
		s.execIgnoreDuplicate(stmt)
	}

	return nil
}

// execIgnoreDuplicate executes an ALTER TABLE statement and ignores "duplicate column" errors.
func (s *Store) execIgnoreDuplicate(stmt string) {
	_, err := s.db.Exec(stmt)
	if err != nil {
		// SQLite returns "duplicate column name" when column already exists
		if strings.Contains(err.Error(), "duplicate column") {
			return
		}
		// Ignore other ALTER TABLE errors on existing columns
	}
}

// --- Collections ---

func (s *Store) CreateCollection(name, typ, path, pattern string) (*Collection, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO collections (name, type, path, pattern, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		name, typ, path, pattern, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Collection{ID: id, Name: name, Type: typ, Path: path, Pattern: pattern}, nil
}

func (s *Store) GetCollectionByName(name string) (*Collection, error) {
	c := &Collection{}
	err := s.db.QueryRow(
		`SELECT id, name, type, path, pattern, created_at, updated_at FROM collections WHERE name = ?`, name,
	).Scan(&c.ID, &c.Name, &c.Type, &c.Path, &c.Pattern, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ListCollections() ([]Collection, error) {
	rows, err := s.db.Query(`SELECT id, name, type, path, pattern, created_at, updated_at FROM collections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Path, &c.Pattern, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, nil
}

func (s *Store) DeleteCollection(id int64) error {
	// Delete FTS entries for documents in this collection
	_, err := s.db.Exec(`DELETE FROM documents_fts WHERE rowid IN (SELECT id FROM documents WHERE collection_id = ?)`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM chunks WHERE document_id IN (SELECT id FROM documents WHERE collection_id = ?)`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM documents WHERE collection_id = ?`, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM collections WHERE id = ?`, id)
	return err
}

// --- Documents ---

func (s *Store) GetDocument(collectionID int64, path string) (*Document, error) {
	d := &Document{}
	err := s.db.QueryRow(
		`SELECT id, collection_id, path, title, content_hash, mtime, line_count FROM documents WHERE collection_id = ? AND path = ?`,
		collectionID, path,
	).Scan(&d.ID, &d.CollectionID, &d.Path, &d.Title, &d.ContentHash, &d.Mtime, &d.LineCount)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) UpsertDocument(collectionID int64, path, title, contentHash string, mtime float64, lineCount int) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO documents (collection_id, path, title, content_hash, mtime, line_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(collection_id, path) DO UPDATE SET
		   title = excluded.title,
		   content_hash = excluded.content_hash,
		   mtime = excluded.mtime,
		   line_count = excluded.line_count,
		   updated_at = excluded.updated_at`,
		collectionID, path, title, contentHash, mtime, lineCount, now, now,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	// If it was an update, we need to get the actual ID
	if id == 0 {
		err = s.db.QueryRow(`SELECT id FROM documents WHERE collection_id = ? AND path = ?`, collectionID, path).Scan(&id)
		if err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ListDocumentPaths returns all document paths for a collection.
func (s *Store) ListDocumentPaths(collectionID int64) (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT id, path FROM documents WHERE collection_id = ?`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int64)
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		m[path] = id
	}
	return m, nil
}

// DeleteDocument removes a document and its chunks/FTS entries.
func (s *Store) DeleteDocument(docID int64) error {
	s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID)
	s.db.Exec(`DELETE FROM chunks WHERE document_id = ?`, docID)
	_, err := s.db.Exec(`DELETE FROM documents WHERE id = ?`, docID)
	return err
}

func (s *Store) UpdateDocumentMtime(docID int64, mtime float64) error {
	_, err := s.db.Exec(`UPDATE documents SET mtime = ? WHERE id = ?`, mtime, docID)
	return err
}

// --- FTS ---

func (s *Store) UpsertFTS(docID int64, title, content string) error {
	// Delete existing then insert (FTS5 doesn't support upsert)
	s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID)
	_, err := s.db.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`, docID, title, content)
	return err
}

// AppendFTS appends content to an existing FTS entry, preserving earlier text and title.
func (s *Store) AppendFTS(docID int64, newContent string) error {
	var existingTitle, existingContent string
	err := s.db.QueryRow(`SELECT title, content FROM documents_fts WHERE rowid = ?`, docID).Scan(&existingTitle, &existingContent)
	if err != nil {
		// No existing entry — just insert
		_, err = s.db.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, '', ?)`, docID, newContent)
		return err
	}
	combined := existingContent + "\n" + newContent
	s.db.Exec(`DELETE FROM documents_fts WHERE rowid = ?`, docID)
	_, err = s.db.Exec(`INSERT INTO documents_fts (rowid, title, content) VALUES (?, ?, ?)`, docID, existingTitle, combined)
	return err
}

func (s *Store) SearchFTS(query string, limit int) ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT d.id, d.title, d.path, c.name, snippet(documents_fts, 1, '>>>', '<<<', '...', 40) as snip, bm25(documents_fts)
		 FROM documents_fts f
		 JOIN documents d ON d.id = f.rowid
		 JOIN collections c ON c.id = d.collection_id
		 WHERE documents_fts MATCH ?
		 ORDER BY bm25(documents_fts)
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.DocumentID, &r.Title, &r.Path, &r.Collection, &r.Content, &r.Score); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// --- Chunks ---

func (s *Store) DeleteChunksForDocument(docID int64) error {
	_, err := s.db.Exec(`DELETE FROM chunks WHERE document_id = ?`, docID)
	return err
}

func (s *Store) InsertChunk(docID int64, seq int, content string, embedding []float32) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO chunks (document_id, seq, content, embedding, chunk_type, image_path, created_at) VALUES (?, ?, ?, ?, 0, NULL, ?)`,
		docID, seq, content, encodeEmbedding(embedding), now,
	)
	return err
}

// InsertImageChunk inserts an image chunk with type=1 and an image path.
func (s *Store) InsertImageChunk(docID int64, seq int, context string, imagePath string, embedding []float32) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO chunks (document_id, seq, content, embedding, chunk_type, image_path, created_at) VALUES (?, ?, ?, ?, 1, ?, ?)`,
		docID, seq, context, encodeEmbedding(embedding), imagePath, now,
	)
	return err
}

func (s *Store) SearchVector(queryEmb []float32, limit int) ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT ch.id, ch.document_id, ch.content, ch.embedding,
		        COALESCE(ch.chunk_type, 0), COALESCE(ch.image_path, ''),
		        d.title, d.path, c.name
		 FROM chunks ch
		 JOIN documents d ON d.id = ch.document_id
		 JOIN collections c ON c.id = d.collection_id
		 WHERE ch.embedding IS NOT NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		SearchResult
		score float64
	}
	var all []scored

	for rows.Next() {
		var (
			chunkID    int64
			docID      int64
			content    string
			embBlob    []byte
			chunkType  int
			imagePath  string
			title      string
			path       string
			collection string
		)
		if err := rows.Scan(&chunkID, &docID, &content, &embBlob, &chunkType, &imagePath, &title, &path, &collection); err != nil {
			return nil, err
		}
		emb := decodeEmbedding(embBlob)
		sim := cosineSimilarity(queryEmb, emb)
		all = append(all, scored{
			SearchResult: SearchResult{
				ChunkID:    chunkID,
				DocumentID: docID,
				Title:      title,
				Path:       path,
				Collection: collection,
				Content:    content,
				ChunkType:  chunkType,
				ImagePath:  imagePath,
			},
			score: sim,
		})
	}

	// Sort by similarity descending
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].score > all[i].score {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	if len(all) > limit {
		all = all[:limit]
	}

	results := make([]SearchResult, len(all))
	for i, s := range all {
		s.SearchResult.Score = s.score
		results[i] = s.SearchResult
	}
	return results, nil
}

func (s *Store) UpdateChunkEmbedding(chunkID int64, embedding []float32) error {
	_, err := s.db.Exec(`UPDATE chunks SET embedding = ? WHERE id = ?`, encodeEmbedding(embedding), chunkID)
	return err
}

// GetChunksWithoutEmbedding returns chunks that don't have embeddings yet.
// If force is true, returns all chunks.
func (s *Store) GetChunksWithoutEmbedding(force bool) ([]Chunk, error) {
	query := `SELECT id, document_id, seq, content, COALESCE(chunk_type, 0), COALESCE(image_path, '') FROM chunks WHERE embedding IS NULL`
	if force {
		query = `SELECT id, document_id, seq, content, COALESCE(chunk_type, 0), COALESCE(image_path, '') FROM chunks`
	}
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []Chunk
	for rows.Next() {
		var ch Chunk
		if err := rows.Scan(&ch.ID, &ch.DocumentID, &ch.Seq, &ch.Content, &ch.ChunkType, &ch.ImagePath); err != nil {
			return nil, err
		}
		chunks = append(chunks, ch)
	}
	return chunks, nil
}

// --- Stats ---

func (s *Store) CountDocuments(collectionID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE collection_id = ?`, collectionID).Scan(&n)
	return n, err
}

func (s *Store) CountChunks(collectionID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM chunks WHERE document_id IN (SELECT id FROM documents WHERE collection_id = ?)`,
		collectionID,
	).Scan(&n)
	return n, err
}

// --- Helpers ---

func encodeEmbedding(v []float32) []byte {
	if v == nil {
		return nil
	}
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeEmbedding(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
