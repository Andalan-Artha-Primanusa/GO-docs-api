package app

import (
	"database/sql"
	"encoding/json"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRequestSummary(row rowScanner, s *RequestSummary) error {
	var lastApproverName, nextApproverName sql.NullString
	var lastApprovedAt, nextApprovalCreatedAt, nextApprovalDueAt sql.NullTime
	var nextApprovalLevel sql.NullInt64
	if err := row.Scan(
		&s.ID, &s.TicketNumber, &s.RequestTypeID, &s.RequestTypeName, &s.UserID, &s.OverallStatus, &s.CurrentStage, &s.DueAt,
		&s.IsOverdue, &s.CreatedAt, &s.ApprovalTotal, &s.ApprovedTotal, &lastApproverName, &lastApprovedAt,
		&nextApproverName, &nextApprovalLevel, &nextApprovalCreatedAt, &nextApprovalDueAt,
	); err != nil {
		return err
	}
	if lastApproverName.Valid {
		s.LastApproverName = &lastApproverName.String
	}
	if lastApprovedAt.Valid {
		s.LastApprovedAt = &lastApprovedAt.Time
	}
	if nextApproverName.Valid {
		s.NextApproverName = &nextApproverName.String
	}
	if nextApprovalLevel.Valid {
		level := int(nextApprovalLevel.Int64)
		s.NextApprovalLevel = &level
	}
	if nextApprovalCreatedAt.Valid {
		s.NextApprovalCreatedAt = &nextApprovalCreatedAt.Time
	}
	if nextApprovalDueAt.Valid {
		s.NextApprovalDueAt = &nextApprovalDueAt.Time
	}
	return nil
}

func (a *App) canAccessRequest(userID, requestID int64) (bool, error) {
	if ok, err := a.hasPermission(userID, "request.view_all"); ok || err != nil {
		if !ok || err != nil {
			return ok, err
		}
		return a.requestIsActive(requestID)
	}
	var exists int
	err := a.db.QueryRow(`SELECT 1 FROM requests WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, requestID, userID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	err = a.db.QueryRow(`
		SELECT 1
		FROM approvals ap
		JOIN requests req ON req.id = ap.request_id
		WHERE ap.request_id = ? AND ap.approver_id = ? AND req.deleted_at IS NULL`, requestID, userID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	return a.isPICForRequest(userID, requestID)
}

func (a *App) requestIsActive(requestID int64) (bool, error) {
	var exists int
	err := a.db.QueryRow(`SELECT 1 FROM requests WHERE id = ? AND deleted_at IS NULL`, requestID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (a *App) isPICForRequest(userID, requestID int64) (bool, error) {
	var exists int
	err := a.db.QueryRow(`
		SELECT 1
		FROM requests req
		JOIN request_type_pic rtp ON rtp.request_type_id = req.request_type_id
		WHERE req.id = ? AND rtp.user_id = ? AND req.deleted_at IS NULL
		LIMIT 1`, requestID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (a *App) requestOwner(requestID int64) int64 {
	var id int64
	_ = a.db.QueryRow(`SELECT user_id FROM requests WHERE id = ? AND deleted_at IS NULL`, requestID).Scan(&id)
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
		WHERE req.id = ? AND req.deleted_at IS NULL`, requestID)
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
	err := scanRequestSummary(a.db.QueryRow(`
		SELECT req.id, req.ticket_number, req.request_type_id, rt.name, req.user_id, req.overall_status, req.current_stage, req.due_at,
		       (req.due_at IS NOT NULL AND req.due_at < NOW() AND req.overall_status NOT IN ('done','rejected')) is_overdue, req.created_at,
		       (SELECT COUNT(*) FROM approvals ap WHERE ap.request_id = req.id) approval_total,
		       (SELECT COUNT(*) FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'approve') approved_total,
		       (SELECT u.name FROM approvals ap JOIN users u ON u.id = ap.approver_id WHERE ap.request_id = req.id AND ap.action = 'approve' ORDER BY ap.acted_at DESC, ap.level DESC LIMIT 1) last_approver_name,
		       (SELECT ap.acted_at FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'approve' ORDER BY ap.acted_at DESC, ap.level DESC LIMIT 1) last_approved_at,
		       (SELECT u.name FROM approvals ap JOIN users u ON u.id = ap.approver_id WHERE ap.request_id = req.id AND ap.action = 'pending' ORDER BY ap.level ASC LIMIT 1) next_approver_name,
		       (SELECT ap.level FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'pending' ORDER BY ap.level ASC LIMIT 1) next_approval_level,
		       (SELECT ap.created_at FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'pending' ORDER BY ap.level ASC LIMIT 1) next_approval_created_at,
		       (SELECT ap.due_at FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'pending' ORDER BY ap.level ASC LIMIT 1) next_approval_due_at
		FROM requests req JOIN request_types rt ON rt.id = req.request_type_id
		WHERE req.id = ? AND req.deleted_at IS NULL`, id), &summary)
	if err != nil {
		return nil, err
	}
	summary.fillLabels()

	fields, err := a.jsonRows(`SELECT field_key, field_value FROM request_fields WHERE request_id = ?`, id)
	if err != nil {
		return nil, err
	}
	approvals, err := a.mapRows(`SELECT ap.id, ap.approver_id, u.name approver_name, GROUP_CONCAT(r.name ORDER BY r.name SEPARATOR ', ') approver_roles, ap.level, ap.action, ap.note, ap.acted_at, ap.due_at, ap.created_at FROM approvals ap JOIN users u ON u.id = ap.approver_id LEFT JOIN user_roles ur ON ur.user_id = u.id LEFT JOIN roles r ON r.id = ur.role_id WHERE ap.request_id = ? GROUP BY ap.id, ap.approver_id, u.name, ap.level, ap.action, ap.note, ap.acted_at, ap.due_at, ap.created_at ORDER BY ap.level`, id)
	if err != nil {
		return nil, err
	}
	timeline, err := a.mapRows(`SELECT rsl.id, rsl.stage_number, rsl.status_text, rsl.updated_by_user_id, u.name updated_by_name, rsl.note, rsl.created_at FROM request_status_log rsl JOIN users u ON u.id = rsl.updated_by_user_id WHERE rsl.request_id = ? ORDER BY rsl.id`, id)
	if err != nil {
		return nil, err
	}
	comments, err := a.mapRows(`SELECT rc.id, rc.user_id, u.name user_name, rc.message, rc.created_at FROM request_comments rc JOIN users u ON u.id = rc.user_id WHERE rc.request_id = ? AND rc.deleted_at IS NULL ORDER BY rc.id`, id)
	if err != nil {
		return nil, err
	}
	results, err := a.mapRows(`SELECT rr.id, rr.result_text, rr.given_by_user_id, u.name given_by_name, rr.created_at FROM request_results rr JOIN users u ON u.id = rr.given_by_user_id WHERE rr.request_id = ? AND rr.deleted_at IS NULL ORDER BY rr.id`, id)
	if err != nil {
		return nil, err
	}
	attachments, err := a.mapRows(`SELECT att.id, att.source_type, att.source_id, att.file_url, att.file_name, att.mime_type, att.file_size, att.uploaded_by_user_id, u.name uploaded_by_name, att.uploaded_at FROM attachments att LEFT JOIN users u ON u.id = att.uploaded_by_user_id WHERE att.request_id = ? AND att.deleted_at IS NULL ORDER BY att.id`, id)
	if err != nil {
		return nil, err
	}
	events, err := a.buildTimeline(id)
	if err != nil {
		return nil, err
	}
	currentStep, err := a.currentStep(summary)
	if err != nil {
		return nil, err
	}
	picStage, err := a.picStageSummary(id, summary.RequestTypeID, summary.CurrentStage)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"request": summary, "fields": fields, "approvals": approvals,
		"current_step": currentStep, "pic_stage": picStage,
		"timeline": timeline, "events": events, "comments": comments, "results": results, "attachments": attachments,
	}, nil
}

func (a *App) currentStep(summary RequestSummary) (map[string]any, error) {
	if summary.OverallStatus == "in_approval" {
		row := a.db.QueryRow(`
			SELECT ap.level, ap.action, ap.due_at, ap.approver_id, u.name
			FROM approvals ap
			JOIN users u ON u.id = ap.approver_id
			WHERE ap.request_id = ? AND ap.action = 'pending'
			ORDER BY ap.level LIMIT 1`, summary.ID)
		var level int
		var action string
		var dueAt any
		var actorID int64
		var actorName string
		if err := row.Scan(&level, &action, &dueAt, &actorID, &actorName); err != nil {
			if err == sql.ErrNoRows {
				return map[string]any{"type": "approval", "status": "empty"}, nil
			}
			return nil, err
		}
		return map[string]any{"type": "approval", "level": level, "status": action, "actor_user_id": actorID, "actor_name": actorName, "due_at": dueAt}, nil
	}
	if summary.OverallStatus == "in_progress" {
		stage, err := a.picStageSummary(summary.ID, summary.RequestTypeID, summary.CurrentStage)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "pic", "status": "active", "stage": summary.CurrentStage, "pic": stage}, nil
	}
	return map[string]any{"type": summary.OverallStatus, "status": summary.OverallStatus}, nil
}

func (a *App) picStageSummary(requestID, requestTypeID int64, currentStage int) (map[string]any, error) {
	rows, err := a.db.Query(`
		SELECT rtp.stage_number, rtp.user_id, u.name
		FROM request_type_pic rtp
		JOIN users u ON u.id = rtp.user_id
		WHERE rtp.request_type_id = ? AND rtp.stage_number = ?
		ORDER BY u.name`, requestTypeID, currentStage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []map[string]any
	for rows.Next() {
		var stage int
		var userID int64
		var name string
		if err := rows.Scan(&stage, &userID, &name); err != nil {
			return nil, err
		}
		users = append(users, map[string]any{"user_id": userID, "name": name})
	}
	return map[string]any{"request_id": requestID, "stage_number": currentStage, "users": users}, rows.Err()
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
	if arg == nil {
		return a.mapRowsArgs(query)
	}
	return a.mapRowsArgs(query, arg)
}

func (a *App) mapRowsArgs(query string, args ...any) ([]map[string]any, error) {
	rows, err := a.db.Query(query, args...)
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

func (a *App) buildTimeline(requestID int64) ([]map[string]any, error) {
	return a.mapRowsArgs(`
		SELECT 'request_created' event_type, req.id source_id, 'Pengajuan dibuat' title, req.ticket_number description, req.user_id actor_user_id, u.name actor_name, req.created_at occurred_at
		FROM requests req
		JOIN users u ON u.id = req.user_id
		WHERE req.id = ?
		UNION ALL
		SELECT CASE WHEN ap.action = 'pending' THEN 'approval_requested' WHEN ap.action = 'approve' THEN 'approved' ELSE 'rejected' END event_type,
		       ap.id source_id,
		       CASE WHEN ap.action = 'pending' THEN CONCAT('Menunggu approval level ', ap.level) WHEN ap.action = 'approve' THEN CONCAT('Approved level ', ap.level) ELSE CONCAT('Rejected level ', ap.level) END title,
		       ap.note description, ap.approver_id actor_user_id, u.name actor_name, COALESCE(ap.acted_at, ap.created_at) occurred_at
		FROM approvals ap
		JOIN users u ON u.id = ap.approver_id
		WHERE ap.request_id = ?
		UNION ALL
		SELECT 'progress_update' event_type, rsl.id source_id, rsl.status_text title, rsl.note description, rsl.updated_by_user_id actor_user_id, u.name actor_name, rsl.created_at occurred_at
		FROM request_status_log rsl
		JOIN users u ON u.id = rsl.updated_by_user_id
		WHERE rsl.request_id = ?
		UNION ALL
		SELECT 'comment' event_type, rc.id source_id, 'Komentar' title, rc.message description, rc.user_id actor_user_id, u.name actor_name, rc.created_at occurred_at
		FROM request_comments rc
		JOIN users u ON u.id = rc.user_id
		WHERE rc.request_id = ? AND rc.deleted_at IS NULL
		UNION ALL
		SELECT 'result' event_type, rr.id source_id, 'Hasil diberikan' title, rr.result_text description, rr.given_by_user_id actor_user_id, u.name actor_name, rr.created_at occurred_at
		FROM request_results rr
		JOIN users u ON u.id = rr.given_by_user_id
		WHERE rr.request_id = ? AND rr.deleted_at IS NULL
		UNION ALL
		SELECT 'attachment' event_type, att.id source_id, 'Lampiran diunggah' title, COALESCE(att.file_name, att.file_url) description, att.uploaded_by_user_id actor_user_id, u.name actor_name, att.uploaded_at occurred_at
		FROM attachments att
		LEFT JOIN users u ON u.id = att.uploaded_by_user_id
		WHERE att.request_id = ? AND att.deleted_at IS NULL
		ORDER BY occurred_at ASC, source_id ASC`, requestID, requestID, requestID, requestID, requestID, requestID)
}
