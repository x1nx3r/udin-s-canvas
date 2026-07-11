package lib

import (
	"database/sql"
	"log"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	DB          *sql.DB
	dbPath      string
	storageRoot string
	stopWAL     chan struct{}
)

func GetDBPath() string {
	return dbPath
}

func StorageRoot() string {
	return storageRoot
}

func StopWAL() {
	if stopWAL != nil {
		close(stopWAL)
	}
}

// FilePath returns the on-disk path for a drawing's file blob.
func FilePath(drawingID, fileID string) string {
	return filepath.Join(storageRoot, drawingID, fileID)
}

func InitDB(path string) {
	dbPath = path

	// Derive storage root from DB path: replace the filename with "files/".
	// ./canvas.db  ->  ./files/
	storageRoot = filepath.Join(filepath.Dir(path), "files")

	var err error
	DB, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatalf("ping sqlite: %v", err)
	}

	DB.SetMaxOpenConns(1)

	migrate()

	// Periodically checkpoint the WAL so it doesn't balloon between restarts.
	// PASSIVE mode never blocks readers or writers.
	stopWAL = make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := DB.Exec("PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
					log.Printf("wal_checkpoint: %v", err)
				}
			case <-stopWAL:
				return
			}
		}
	}()

	log.Println("SQLite initialized:", dbPath)
}

func migrate() {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS drawings (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT 'Untitled',
			content TEXT,
			share_slug TEXT UNIQUE,
			thumbnail TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_drawings_owner ON drawings(owner_id);
		CREATE INDEX IF NOT EXISTS idx_drawings_share_slug ON drawings(share_slug);

		-- Thumbnails live in their own table so list queries don't
		-- drag up to 100KB of base64 PNG per row on the main drawings table.
		CREATE TABLE IF NOT EXISTS drawing_thumbnails (
			drawing_id TEXT PRIMARY KEY REFERENCES drawings(id) ON DELETE CASCADE,
			data       TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- VIP whitelist for closed-beta multiplayer access.
		CREATE TABLE IF NOT EXISTS feature_whitelist (
			email      TEXT PRIMARY KEY,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- User tracking for admin panel.
		CREATE TABLE IF NOT EXISTS users (
			uid        TEXT PRIMARY KEY,
			email      TEXT,
			name       TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_seen  DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// One-time idempotent migration: move existing thumbnail blobs out of the
	// drawings table into drawing_thumbnails. Safe to run on every startup —
	// INSERT OR IGNORE is a no-op for rows already migrated.
	_, err = DB.Exec(`
		INSERT OR IGNORE INTO drawing_thumbnails (drawing_id, data, updated_at)
			SELECT id, thumbnail, updated_at FROM drawings
			WHERE thumbnail IS NOT NULL AND thumbnail != '';
		UPDATE drawings SET thumbnail = NULL
			WHERE thumbnail IS NOT NULL AND thumbnail != '';
	`)
	if err != nil {
		log.Fatalf("thumbnail migration: %v", err)
	}

	// Idempotent: add allow_public_edits column for multiplayer opt-in.
	// SQLite does not support IF NOT EXISTS on ALTER TABLE, so we check manually.
	var colCount int
	_ = DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('drawings') WHERE name='allow_public_edits'`).Scan(&colCount)
	if colCount == 0 {
		if _, err = DB.Exec(`ALTER TABLE drawings ADD COLUMN allow_public_edits INTEGER NOT NULL DEFAULT 0`); err != nil {
			log.Fatalf("migrate allow_public_edits: %v", err)
		}
	}

	// Idempotent: add last_ip column to users for admin panel.
	_ = DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='last_ip'`).Scan(&colCount)
	if colCount == 0 {
		if _, err = DB.Exec(`ALTER TABLE users ADD COLUMN last_ip TEXT`); err != nil {
			log.Fatalf("migrate users.last_ip: %v", err)
		}
	}

	// File storage: table for blobs attached to drawings.
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS drawing_files (
			id         TEXT NOT NULL,
			drawing_id TEXT NOT NULL REFERENCES drawings(id) ON DELETE CASCADE,
			owner_id   TEXT NOT NULL,
			mime_type  TEXT NOT NULL,
			file_size  INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (drawing_id, id)
		);
	`); err != nil {
		log.Fatalf("migrate drawing_files: %v", err)
	}

	// Per-user storage quota columns.
	_ = DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='storage_max_bytes'`).Scan(&colCount)
	if colCount == 0 {
		if _, err = DB.Exec(`ALTER TABLE users ADD COLUMN storage_max_bytes INTEGER NOT NULL DEFAULT 31457280`); err != nil {
			log.Fatalf("migrate users.storage_max_bytes: %v", err)
		}
	}
	_ = DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='storage_used_bytes'`).Scan(&colCount)
	if colCount == 0 {
		if _, err = DB.Exec(`ALTER TABLE users ADD COLUMN storage_used_bytes INTEGER NOT NULL DEFAULT 0`); err != nil {
			log.Fatalf("migrate users.storage_used_bytes: %v", err)
		}
	}
}
