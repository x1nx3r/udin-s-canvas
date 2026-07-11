package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gotth/app/lib"
)

type sceneData struct {
	Elements json.RawMessage `json:"elements"`
	AppState json.RawMessage `json:"appState"`
}

func generateSlug() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func checkOwnership(r *http.Request, id string) bool {
	uid := lib.GetUserUID(r.Context())
	if uid == "" {
		return false
	}
	var ownerId string
	err := lib.DB.QueryRowContext(r.Context(), "SELECT owner_id FROM drawings WHERE id = ?", id).Scan(&ownerId)
	return err == nil && ownerId == uid
}

func DataHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}

	var content string
	err := lib.DB.QueryRowContext(r.Context(), "SELECT content FROM drawings WHERE id = ?", id).Scan(&content)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	writeSceneWithFiles(w, id, content)
}

func SaveHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 5*1024*1024) // 5 MB limit
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	var sd sceneData
	if err := json.Unmarshal(body, &sd); err != nil {
		http.Error(w, "invalid scene data", http.StatusBadRequest)
		return
	}

	_, err = lib.DB.ExecContext(r.Context(),
		"UPDATE drawings SET content = ?, updated_at = ? WHERE id = ?",
		string(body), time.Now(), id)
	if err != nil {
		log.Printf("save drawing %s: %v", id, err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func ShareHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := lib.GetUserUID(r.Context())

	var ownerId string
	var existingSlug sql.NullString
	err := lib.DB.QueryRowContext(r.Context(), "SELECT owner_id, share_slug FROM drawings WHERE id = ?", id).Scan(&ownerId, &existingSlug)
	if err != nil || ownerId != uid {
		http.NotFound(w, r)
		return
	}

	if existingSlug.Valid && existingSlug.String != "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"slug": existingSlug.String})
		return
	}

	slug, err := generateSlug()
	if err != nil {
		http.Error(w, "slug generation failed", http.StatusInternalServerError)
		return
	}

	_, err = lib.DB.ExecContext(r.Context(),
		"UPDATE drawings SET share_slug = ?, updated_at = ? WHERE id = ?",
		slug, time.Now(), id)
	if err != nil {
		http.Error(w, "share creation failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"slug": slug})
}

func RenameHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		http.Error(w, "missing or invalid title", http.StatusBadRequest)
		return
	}

	_, err := lib.DB.ExecContext(r.Context(),
		"UPDATE drawings SET title = ?, updated_at = ? WHERE id = ?",
		body.Title, time.Now(), id)
	if err != nil {
		http.Error(w, "rename failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func ThumbnailHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 256*1024) // 256 KB limit
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "thumbnail too large", http.StatusRequestEntityTooLarge)
		return
	}

	_, err = lib.DB.ExecContext(r.Context(),
		"INSERT OR REPLACE INTO drawing_thumbnails (drawing_id, data, updated_at) VALUES (?, ?, datetime('now'))",
		id, string(body))
	if err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func SaveFileHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024) // 10 MB limit
	var body struct {
		FileID   string `json:"fileId"`
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.FileID == "" || body.MimeType == "" || body.Data == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	// Decode base64 data URL: "data:image/webp;base64,..."
	raw := body.Data
	if idx := strings.Index(raw, "base64,"); idx >= 0 {
		raw = raw[idx+7:]
	}
	blob, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		http.Error(w, "invalid base64", http.StatusBadRequest)
		return
	}
	fileSize := int64(len(blob))

	uid := lib.GetUserUID(r.Context())

	// Check quota (unless unlimited).
	tx, err := lib.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var maxBytes, usedBytes int64
	err = tx.QueryRowContext(r.Context(),
		`SELECT storage_max_bytes, storage_used_bytes FROM users WHERE uid = ?`, uid,
	).Scan(&maxBytes, &usedBytes)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if maxBytes >= 0 && usedBytes+fileSize > maxBytes {
		http.Error(w, "quota exceeded", http.StatusInsufficientStorage)
		return
	}

	// Write to disk.
	dir := filepath.Join(lib.StorageRoot(), id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("mkdir files %s: %v", id, err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, body.FileID), blob, 0644); err != nil {
		log.Printf("write file %s/%s: %v", id, body.FileID, err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Insert DB row and update quota in the same transaction.
	_, err = tx.ExecContext(r.Context(),
		`INSERT OR REPLACE INTO drawing_files (id, drawing_id, owner_id, mime_type, file_size) VALUES (?, ?, ?, ?, ?)`,
		body.FileID, id, uid, body.MimeType, fileSize,
	)
	if err != nil {
		log.Printf("insert drawing_file %s/%s: %v", id, body.FileID, err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(),
		`UPDATE users SET storage_used_bytes = storage_used_bytes + ? WHERE uid = ?`, fileSize, uid,
	)
	if err != nil {
		log.Printf("update quota %s: %v", uid, err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("commit file tx %s/%s: %v", id, body.FileID, err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func DeleteFileHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}

	fileID := r.URL.Query().Get("fileId")
	if fileID == "" {
		http.Error(w, "missing fileId", http.StatusBadRequest)
		return
	}

	uid := lib.GetUserUID(r.Context())

	tx, err := lib.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var fileSize int64
	err = tx.QueryRowContext(r.Context(),
		`DELETE FROM drawing_files WHERE drawing_id = ? AND id = ? AND owner_id = ? RETURNING file_size`,
		id, fileID, uid,
	).Scan(&fileSize)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Remove from disk (best-effort).
	os.Remove(filepath.Join(lib.StorageRoot(), id, fileID))

	_, err = tx.ExecContext(r.Context(),
		`UPDATE users SET storage_used_bytes = MAX(0, storage_used_bytes - ?) WHERE uid = ?`, fileSize, uid,
	)
	if err != nil {
		log.Printf("update quota on delete %s: %v", uid, err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("commit delete file tx %s/%s: %v", id, fileID, err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func PublicEditHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}

	// Gate: only VIP-whitelisted users can toggle public edit.
	email := lib.GetUserEmail(r.Context())
	var vipCount int
	_ = lib.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM feature_whitelist WHERE email = ?`, email,
	).Scan(&vipCount)
	if vipCount == 0 {
		http.Error(w, "feature not available", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 128)
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	val := 0
	if body.Enabled {
		val = 1
	}
	if _, err := lib.DB.ExecContext(r.Context(),
		"UPDATE drawings SET allow_public_edits = ?, updated_at = ? WHERE id = ?",
		val, time.Now(), id); err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !checkOwnership(r, id) {
		http.NotFound(w, r)
		return
	}

	uid := lib.GetUserUID(r.Context())

	// Collect file sizes before CASCADE delete removes the rows.
	var totalSize int64
	_ = lib.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(file_size), 0) FROM drawing_files WHERE drawing_id = ? AND owner_id = ?`,
		id, uid,
	).Scan(&totalSize)

	// Remove files from disk (best-effort).
	os.RemoveAll(filepath.Join(lib.StorageRoot(), id))

	_, err := lib.DB.ExecContext(r.Context(), "DELETE FROM drawings WHERE id = ?", id)
	if err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}

	// Adjust quota (best-effort; deletion already happened).
	if totalSize > 0 {
		lib.DB.ExecContext(r.Context(),
			`UPDATE users SET storage_used_bytes = MAX(0, storage_used_bytes - ?) WHERE uid = ?`,
			totalSize, uid,
		)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func sanitizeSceneJSON(raw string) []byte {
	if raw == "" {
		raw = `{"elements":[],"appState":{}}`
	}

	var parsed struct {
		Elements json.RawMessage `json:"elements"`
		AppState map[string]any  `json:"appState"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return []byte(`{"elements":[],"appState":{}}`)
	}

	if parsed.AppState != nil {
		parsed.AppState["collaborators"] = []any{}
	}

	sanitized, _ := json.Marshal(parsed)
	return sanitized
}

func writeSceneWithFiles(w http.ResponseWriter, drawingID, content string) {
	sanitized := sanitizeSceneJSON(content)

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(sanitized, &parsed); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(sanitized)
		return
	}

	// Load files from disk and embed as data URLs.
	files := buildFilesMap(drawingID)
	fileJSON, _ := json.Marshal(files)
	parsed["files"] = fileJSON

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(parsed)
}

// buildFilesMap reads all files for a drawing from disk and returns
// a map of fileId -> data:... URL (base64).
func buildFilesMap(drawingID string) map[string]string {
	rows, err := lib.DB.Query(
		`SELECT id, mime_type FROM drawing_files WHERE drawing_id = ?`, drawingID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	files := make(map[string]string)
	base := filepath.Join(lib.StorageRoot(), drawingID)

	for rows.Next() {
		var fileID, mimeType string
		if err := rows.Scan(&fileID, &mimeType); err != nil {
			continue
		}
		blob, err := os.ReadFile(filepath.Join(base, fileID))
		if err != nil {
			continue
		}
		dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(blob)
		files[fileID] = dataURL
	}
	return files
}
