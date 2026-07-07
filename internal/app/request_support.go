package app

import (
	"database/sql"
	"encoding/json"
)

func (a *App) canAccessRequest(userID, requestID int64) (bool, error) {
	if ok, err := a.hasPermission(userID, "request.view_all"); ok || err != nil {
		return ok, err
	}
	var exists int
	err := a.db.QueryRow(`SELECT 1 FROM requests WHERE id = ? AND user_id = ?`, requestID, userID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	err = a.db.QueryRow(`SELECT 1 FROM approvals WHERE request_id = ? AND approver_id = ?`, requestID, userID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	return a.isPICForRequest(userID, requestID)
}

func (a *App) isPICForRequest(userID, requestID int64) (bool, error) {
	var exists int
	err := a.db.QueryRow(`
		SELECT 1
		FROM requests req
		JOIN request_type_pic rtp ON rtp.request_type_id = req.request_type_id
		WHERE req.id = ? AND rtp.user_id = ?
		LIMIT 1`, requestID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (a *App) requestOwner(requestID int64) int64 {
	var id int64
	_ = a.db.QueryRow(`SELECT user_id FROM requests WHERE id = ?`, requestID).Scan(&id)
	return id
}

func (a *App) notifyPICs(requestTypeID, requestID int64, typ, message string) {
	rows, err := a.db.Query(`SELECT user_id FROM request_type_pic WHERE request_type_id = ?`, requestTypeID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		if rows.Scan(&userID) == nil {
			a.notify(userID, requestID, typ, message)
		}
	}
}

func (a *App) notifyConversationUsers(requestID, senderID int64, typ, message string) {
	seen := map[int64]bool{senderID: true}
	owner := a.requestOwner(requestID)
	if owner != 0 && !seen[owner] {
		a.notify(owner, requestID, typ, message)
		seen[owner] = true
	}
	rows, err := a.db.Query(`
		SELECT DISTINCT rtp.user_id
		FROM requests req
		JOIN request_type_pic rtp ON rtp.request_type_id = req.request_type_id
		WHERE req.id = ?`, requestID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		if rows.Scan(&userID) == nil && !seen[userID] {
			a.notify(userID, requestID, typ, message)
			seen[userID] = true
		}
	}
}

func (a *App) buildRequestDetail(id int64) (map[string]any, error) {
	var summary RequestSummary
	err := a.db.QueryRow(`
		SELECT req.id, req.ticket_number, req.request_type_id, rt.name, req.user_id, req.overall_status, req.current_stage, req.due_at,
		       (req.due_at IS NOT NULL AND req.due_at < NOW() AND req.overall_status NOT IN ('done','rejected')) is_overdue, req.created_at
		FROM requests req JOIN request_types rt ON rt.id = req.request_type_id
		WHERE req.id = ?`, id).
		Scan(&summary.ID, &summary.TicketNumber, &summary.RequestTypeID, &summary.RequestTypeName, &summary.UserID, &summary.OverallStatus, &summary.CurrentStage, &summary.DueAt, &summary.IsOverdue, &summary.CreatedAt)
	if err != nil {
		return nil, err
	}

	fields, err := a.jsonRows(`SELECT field_key, field_value FROM request_fields WHERE request_id = ?`, id)
	if err != nil {
		return nil, err
	}
	approvals, err := a.mapRows(`SELECT id, approver_id, level, action, note, acted_at, created_at FROM approvals WHERE request_id = ? ORDER BY level`, id)
	if err != nil {
		return nil, err
	}
	timeline, err := a.mapRows(`SELECT rsl.id, rsl.stage_number, rsl.status_text, rsl.updated_by_user_id, u.name updated_by_name, rsl.note, rsl.created_at FROM request_status_log rsl JOIN users u ON u.id = rsl.updated_by_user_id WHERE rsl.request_id = ? ORDER BY rsl.id`, id)
	if err != nil {
		return nil, err
	}
	comments, err := a.mapRows(`SELECT rc.id, rc.user_id, u.name user_name, rc.message, rc.created_at FROM request_comments rc JOIN users u ON u.id = rc.user_id WHERE rc.request_id = ? ORDER BY rc.id`, id)
	if err != nil {
		return nil, err
	}
	results, err := a.mapRows(`SELECT rr.id, rr.result_text, rr.given_by_user_id, u.name given_by_name, rr.created_at FROM request_results rr JOIN users u ON u.id = rr.given_by_user_id WHERE rr.request_id = ? ORDER BY rr.id`, id)
	if err != nil {
		return nil, err
	}
	attachments, err := a.mapRows(`SELECT id, source_type, source_id, file_url, uploaded_at FROM attachments WHERE request_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"request": summary, "fields": fields, "approvals": approvals,
		"timeline": timeline, "comments": comments, "results": results, "attachments": attachments,
	}, nil
}

func (a *App) jsonRows(query string, arg any) (map[string]any, error) {
	rows, err := a.db.Query(query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var value any
		_ = json.Unmarshal(raw, &value)
		out[key] = value
	}
	return out, rows.Err()
}

func (a *App) mapRows(query string, arg any) ([]map[string]any, error) {
	var rows *sql.Rows
	var err error
	if arg == nil {
		rows, err = a.db.Query(query)
	} else {
		rows, err = a.db.Query(query, arg)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		item := map[string]any{}
		for i, col := range cols {
			switch v := values[i].(type) {
			case []byte:
				item[col] = string(v)
			default:
				item[col] = v
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
