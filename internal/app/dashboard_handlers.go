package app

import (
	"database/sql"
	"net/http"
)

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	mine, err := a.countByStatus(`WHERE user_id = ?`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	assigned, err := a.countAssignedByStatus(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byType, err := a.countAssignedByType(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"my_requests_by_status":       mine,
		"assigned_requests_by_status": assigned,
		"assigned_requests_by_type":   byType,
	})
}

func (a *App) countByStatus(where string, arg any) ([]map[string]any, error) {
	var rows *sql.Rows
	var err error
	if arg == nil {
		rows, err = a.db.Query(`SELECT overall_status, COUNT(*) total FROM requests ` + where + ` GROUP BY overall_status`)
	} else {
		rows, err = a.db.Query(`SELECT overall_status, COUNT(*) total FROM requests `+where+` GROUP BY overall_status`, arg)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var status string
		var total int64
		if err := rows.Scan(&status, &total); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"status": status, "total": total})
	}
	return out, rows.Err()
}

func (a *App) countAssignedByStatus(userID int64) ([]map[string]any, error) {
	rows, err := a.db.Query(`
		SELECT req.overall_status, COUNT(*) total
		FROM requests req
		JOIN request_type_pic rtp ON rtp.request_type_id = req.request_type_id
		WHERE rtp.user_id = ?
		GROUP BY req.overall_status`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var status string
		var total int64
		if err := rows.Scan(&status, &total); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"status": status, "total": total})
	}
	return out, rows.Err()
}

func (a *App) countAssignedByType(userID int64) ([]map[string]any, error) {
	rows, err := a.db.Query(`
		SELECT rt.id, rt.name, COUNT(*) total
		FROM requests req
		JOIN request_types rt ON rt.id = req.request_type_id
		JOIN request_type_pic rtp ON rtp.request_type_id = req.request_type_id
		WHERE rtp.user_id = ?
		GROUP BY rt.id, rt.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, total int64
		var name string
		if err := rows.Scan(&id, &name, &total); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"request_type_id": id, "request_type_name": name, "total": total})
	}
	return out, rows.Err()
}

func (a *App) notifications(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, request_id, type, message, is_read, created_at FROM notifications WHERE user_id = ? ORDER BY id DESC`, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var requestID *int64
		var typ, message string
		var read bool
		var createdAt any
		if err := rows.Scan(&id, &requestID, &typ, &message, &read, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{"id": id, "request_id": requestID, "type": typ, "message": message, "is_read": read, "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) readNotification(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, err = a.db.Exec(`UPDATE notifications SET is_read = TRUE WHERE id = ? AND user_id = ?`, id, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
