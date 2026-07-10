package app

import (
	"database/sql"
	"net/http"
)

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	mine, err := a.countByStatus(`WHERE user_id = ? AND deleted_at IS NULL`, userID)
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

func (a *App) dashboardAnalytics(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	canViewAll, err := a.hasPermission(userID, "request.view_all")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	scopeWhere := "req.deleted_at IS NULL"
	scopeArgs := []any{}
	if !canViewAll {
		scopeWhere = `req.deleted_at IS NULL AND (
			req.user_id = ?
			OR EXISTS (SELECT 1 FROM approvals ap WHERE ap.request_id = req.id AND ap.approver_id = ?)
			OR EXISTS (SELECT 1 FROM request_type_pic rtp WHERE rtp.request_type_id = req.request_type_id AND rtp.user_id = ?)
		)`
		scopeArgs = []any{userID, userID, userID}
	}
	statusRows, err := a.mapRowsArgs(`SELECT req.overall_status status, COUNT(*) total FROM requests req WHERE `+scopeWhere+` GROUP BY req.overall_status ORDER BY total DESC`, scopeArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	trendRows, err := a.mapRowsArgs(`SELECT DATE(req.created_at) day, COUNT(*) created, SUM(req.overall_status = 'done') done, SUM(req.overall_status = 'rejected') rejected FROM requests req WHERE `+scopeWhere+` AND req.created_at >= DATE_SUB(CURDATE(), INTERVAL 13 DAY) GROUP BY DATE(req.created_at) ORDER BY day ASC`, scopeArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	divisionRows, err := a.mapRowsArgs(`SELECT COALESCE(d.name, 'Tanpa Division') division, COUNT(*) total FROM requests req JOIN request_types rt ON rt.id = req.request_type_id LEFT JOIN departments d ON d.id = rt.department_owner_id WHERE `+scopeWhere+` GROUP BY division ORDER BY total DESC LIMIT 8`, scopeArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bottleneckRows, err := a.mapRowsArgs(`SELECT u.id user_id, u.name approver_name, COUNT(*) pending, MIN(ap.created_at) oldest_pending_at, AVG(TIMESTAMPDIFF(HOUR, ap.created_at, NOW())) avg_wait_hours FROM approvals ap JOIN requests req ON req.id = ap.request_id JOIN users u ON u.id = ap.approver_id WHERE ap.action = 'pending' AND `+scopeWhere+` GROUP BY u.id, u.name ORDER BY pending DESC, avg_wait_hours DESC LIMIT 8`, scopeArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	picRows, err := a.mapRowsArgs(`SELECT u.id user_id, u.name pic_name, COUNT(DISTINCT req.id) assigned, SUM(req.overall_status = 'in_progress') in_progress, SUM(req.overall_status = 'done') done, SUM(req.due_at IS NOT NULL AND req.due_at < NOW() AND req.overall_status NOT IN ('done','rejected')) overdue FROM request_type_pic rtp JOIN users u ON u.id = rtp.user_id JOIN requests req ON req.request_type_id = rtp.request_type_id WHERE `+scopeWhere+` GROUP BY u.id, u.name ORDER BY assigned DESC LIMIT 8`, scopeArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	topForms, err := a.mapRowsArgs(`SELECT rt.id request_type_id, rt.name request_type_name, COUNT(*) total FROM requests req JOIN request_types rt ON rt.id = req.request_type_id WHERE `+scopeWhere+` GROUP BY rt.id, rt.name ORDER BY total DESC LIMIT 8`, scopeArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scope":               map[string]any{"global": canViewAll},
		"status":              statusRows,
		"trend":               trendRows,
		"division":            divisionRows,
		"approval_bottleneck": bottleneckRows,
		"pic_workload":        picRows,
		"top_forms":           topForms,
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
		WHERE rtp.user_id = ? AND req.deleted_at IS NULL
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
		WHERE rtp.user_id = ? AND req.deleted_at IS NULL
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

func (a *App) readRequestNotifications(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_ID", "invalid id")
		return
	}
	_, err = a.db.Exec(`UPDATE notifications SET is_read = TRUE WHERE request_id = ? AND user_id = ?`, id, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
