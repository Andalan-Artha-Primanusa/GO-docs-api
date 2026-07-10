package app

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rbac-request-engine/internal/security"
)

func (a *App) myApprovals(w http.ResponseWriter, r *http.Request) {
	a.listApprovalRows(w, `WHERE ap.approver_id = ? AND ap.action = 'pending'`, currentUserID(r))
}

func (a *App) approvalHistory(w http.ResponseWriter, r *http.Request) {
	a.listApprovalRows(w, `WHERE ap.approver_id = ? AND ap.action <> 'pending'`, currentUserID(r))
}

func (a *App) listApprovalRows(w http.ResponseWriter, where string, arg any) {
	rows, err := a.db.Query(`
		SELECT ap.id, ap.request_id, req.ticket_number, rt.name, ap.level, ap.action, ap.note, ap.acted_at, ap.due_at, ap.created_at
		FROM approvals ap
		JOIN requests req ON req.id = ap.request_id
		JOIN request_types rt ON rt.id = req.request_type_id `+where+` AND req.deleted_at IS NULL
		ORDER BY ap.id DESC`, arg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, requestID int64
		var ticket, typ, action string
		var level int
		var note *string
		var actedAt, dueAt, createdAt any
		if err := rows.Scan(&id, &requestID, &ticket, &typ, &level, &action, &note, &actedAt, &dueAt, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{"id": id, "request_id": requestID, "ticket_number": ticket, "request_type_name": typ, "level": level, "action": action, "note": note, "acted_at": actedAt, "due_at": dueAt, "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	requestID, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if ok, _ := a.canAccessRequest(currentUserID(r), requestID); !ok {
		writeError(w, http.StatusForbidden, "cannot access request")
		return
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	sourceType := r.FormValue("source_type")
	if sourceType == "" {
		sourceType = "form"
	}
	sourceID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	ownerFolder, err := a.uploadOwnerFolder(currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeFilename(header.Filename))
	if err := a.saveUpload(file, ownerFolder, name); err != nil {
		log.Printf("upload failed request_id=%d folder=%s name=%s storage=%s err=%v", requestID, ownerFolder, name, a.cfg.UploadStorage, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("upload saved request_id=%d folder=%s name=%s storage=%s", requestID, ownerFolder, name, a.cfg.UploadStorage)
	fileURL := "/uploads/" + fmt.Sprintf("%s/%s", ownerFolder, name)
	res, err := a.db.Exec(`INSERT INTO attachments (request_id, source_type, source_id, file_url, file_name, mime_type, file_size, uploaded_by_user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		requestID, sourceType, nullableID(sourceID), fileURL, header.Filename, header.Header.Get("Content-Type"), header.Size, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	a.notifyConversationUsers(requestID, currentUserID(r), "attachment_uploaded", "Lampiran baru ditambahkan pada pengajuan")
	a.audit(currentUserID(r), "upload_attachment", "attachment", id, map[string]any{"request_id": requestID, "file_url": fileURL})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "file_url": fileURL, "file_name": header.Filename, "mime_type": header.Header.Get("Content-Type"), "file_size": header.Size})
}

func (a *App) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_ID", "invalid id")
		return
	}
	var requestID, uploaderID int64
	if err := a.db.QueryRow(`SELECT request_id, COALESCE(uploaded_by_user_id, 0) FROM attachments WHERE id = ? AND deleted_at IS NULL`, id).Scan(&requestID, &uploaderID); err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "ATTACHMENT_NOT_FOUND", "attachment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canViewAll, _ := a.hasPermission(currentUserID(r), "request.view_all")
	if uploaderID != currentUserID(r) && !canViewAll {
		writeErrorCode(w, http.StatusForbidden, "ATTACHMENT_FORBIDDEN", "cannot delete this attachment")
		return
	}
	_, err = a.db.Exec(`UPDATE attachments SET deleted_at = NOW() WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.notifyConversationUsers(requestID, currentUserID(r), "attachment_deleted", "Lampiran pada pengajuan dihapus")
	a.audit(currentUserID(r), "soft_delete_attachment", "attachment", id, map[string]any{"request_id": requestID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) deleteResult(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_ID", "invalid id")
		return
	}
	var requestID, giverID int64
	if err := a.db.QueryRow(`SELECT request_id, given_by_user_id FROM request_results WHERE id = ? AND deleted_at IS NULL`, id).Scan(&requestID, &giverID); err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "RESULT_NOT_FOUND", "result not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canViewAll, _ := a.hasPermission(currentUserID(r), "request.view_all")
	if giverID != currentUserID(r) && !canViewAll {
		writeErrorCode(w, http.StatusForbidden, "RESULT_FORBIDDEN", "cannot delete this result")
		return
	}
	_, err = a.db.Exec(`UPDATE request_results SET deleted_at = NOW() WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.notifyConversationUsers(requestID, currentUserID(r), "result_deleted", "Hasil PIC pada pengajuan dihapus")
	a.audit(currentUserID(r), "soft_delete_result", "request_result", id, map[string]any{"request_id": requestID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) downloadUpload(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/uploads/")
	if err := a.serveUpload(w, r, rel); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
}

func (a *App) localUploadRoot() string {
	if a.cfg.UploadDir == "" {
		return "uploads"
	}
	return a.cfg.UploadDir
}

func (a *App) saveUpload(src io.Reader, folder string, name string) error {
	if a.cfg.UploadStorage == "sftp" {
		return a.saveUploadSFTP(src, folder, name)
	}
	if a.cfg.UploadStorage == "ftp" || a.cfg.UploadStorage == "ftps" {
		return a.saveUploadFTP(src, folder, name)
	}
	dir := filepath.Join(a.localUploadRoot(), folder)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	dst, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func (a *App) serveUpload(w http.ResponseWriter, r *http.Request, rel string) error {
	if a.cfg.UploadStorage == "sftp" {
		return a.serveUploadSFTP(w, r, rel)
	}
	if a.cfg.UploadStorage == "ftp" || a.cfg.UploadStorage == "ftps" {
		return a.serveUploadFTP(w, r, rel)
	}
	cleanRel := filepath.Clean(rel)
	if cleanRel == "." || strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return fmt.Errorf("invalid file path")
	}
	http.ServeFile(w, r, filepath.Join(a.localUploadRoot(), cleanRel))
	return nil
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	replacer := strings.NewReplacer(" ", "_", "\\", "_", "/", "_", ":", "_")
	return replacer.Replace(name)
}

func sanitizePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown_user"
	}
	value = sanitizeFilename(value)
	replacer := strings.NewReplacer("@", "_at_", ".", "_", "#", "_", "?", "_", "&", "_", "%", "_")
	value = replacer.Replace(value)
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	return strings.Trim(value, "_")
}

func (a *App) uploadOwnerFolder(userID int64) (string, error) {
	var name string
	if err := a.db.QueryRow(`SELECT name FROM users WHERE id = ?`, userID).Scan(&name); err != nil {
		return "", err
	}
	return sanitizePathSegment(name), nil
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (a *App) auditLogs(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT al.id, al.actor_user_id, u.name actor_name, al.action, al.entity_type, al.entity_id, al.metadata_json, al.created_at
		FROM audit_logs al
		LEFT JOIN users u ON u.id = al.actor_user_id
		WHERE 1=1`
	var args []any
	if v := r.URL.Query().Get("actor_user_id"); v != "" {
		query += ` AND al.actor_user_id = ?`
		args = append(args, v)
	}
	if v := r.URL.Query().Get("action"); v != "" {
		query += ` AND al.action LIKE ?`
		args = append(args, "%"+v+"%")
	}
	if v := r.URL.Query().Get("entity_type"); v != "" {
		query += ` AND al.entity_type = ?`
		args = append(args, v)
	}
	if v := r.URL.Query().Get("entity_id"); v != "" {
		query += ` AND al.entity_id = ?`
		args = append(args, v)
	}
	query += ` ORDER BY al.id DESC LIMIT 200`
	rows, err := a.db.Query(query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var actorID, entityID *int64
		var actorName *string
		var action, entityType string
		var metadata []byte
		var createdAt any
		if err := rows.Scan(&id, &actorID, &actorName, &action, &entityType, &entityID, &metadata, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{"id": id, "actor_user_id": actorID, "actor_name": actorName, "action": action, "entity_type": entityType, "entity_id": entityID, "metadata": string(metadata), "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) requestAuditTimeline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_ID", "invalid id")
		return
	}
	rows, err := a.db.Query(`
		SELECT al.id, al.actor_user_id, u.name actor_name, al.action, al.entity_type, al.entity_id, al.metadata_json, al.created_at
		FROM audit_logs al
		LEFT JOIN users u ON u.id = al.actor_user_id
		WHERE (al.entity_type = 'request' AND al.entity_id = ?)
		   OR JSON_UNQUOTE(JSON_EXTRACT(al.metadata_json, '$.request_id')) = ?
		ORDER BY al.id ASC`, id, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var logID int64
		var actorID, entityID *int64
		var actorName *string
		var action, entityType string
		var metadata []byte
		var createdAt any
		if err := rows.Scan(&logID, &actorID, &actorName, &action, &entityType, &entityID, &metadata, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{"id": logID, "actor_user_id": actorID, "actor_name": actorName, "action": action, "entity_type": entityType, "entity_id": entityID, "metadata": string(metadata), "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) unreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	var total int64
	err := a.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = FALSE`, currentUserID(r)).Scan(&total)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total})
}

func (a *App) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	_, err := a.db.Exec(`UPDATE notifications SET is_read = TRUE WHERE user_id = ?`, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) notificationStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			var total int64
			_ = a.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = FALSE`, currentUserID(r)).Scan(&total)
			_, _ = fmt.Fprintf(w, "event: unread\ndata: %d\n\n", total)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var hash string
	if err := a.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, currentUserID(r)).Scan(&hash); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !security.CheckPassword(req.OldPassword, hash) {
		writeError(w, http.StatusUnauthorized, "old password is invalid")
		return
	}
	nextHash, err := security.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = a.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, nextHash, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "change_password", "user", currentUserID(r), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) adminDashboard(w http.ResponseWriter, r *http.Request) {
	statusRows, _ := a.countByStatus(`WHERE deleted_at IS NULL`, nil)
	byType, _ := a.mapRows(`SELECT rt.name request_type_name, COUNT(*) total FROM requests req JOIN request_types rt ON rt.id = req.request_type_id WHERE req.deleted_at IS NULL GROUP BY rt.id, rt.name ORDER BY total DESC`, nil)
	byDept, _ := a.mapRows(`SELECT COALESCE(d.name, '-') department, COUNT(*) total FROM requests req JOIN users u ON u.id = req.user_id LEFT JOIN departments d ON d.id = u.department_id WHERE req.deleted_at IS NULL GROUP BY department`, nil)
	bySite, _ := a.mapRows(`SELECT COALESCE(sl.name, '-') site_location, COUNT(*) total FROM requests req JOIN users u ON u.id = req.user_id LEFT JOIN site_locations sl ON sl.id = u.site_location_id WHERE req.deleted_at IS NULL GROUP BY site_location`, nil)
	writeJSON(w, http.StatusOK, map[string]any{"requests_by_status": statusRows, "requests_by_type": byType, "requests_by_department": byDept, "requests_by_site": bySite})
}
