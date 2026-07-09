package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type approvalStep struct {
	Level          int     `json:"level"`
	Type           string  `json:"type"`
	UserID         int64   `json:"user_id"`
	Role           string  `json:"role"`
	DepartmentID   int64   `json:"department_id"`
	SiteLocationID int64   `json:"site_location_id"`
	MinAmount      float64 `json:"min_amount"`
	FieldKey       string  `json:"field_key"`
}

type formSchema struct {
	Fields []formField `json:"fields"`
}

type formField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
}

func (a *App) createRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestTypeID int64          `json:"request_type_id"`
		Fields        map[string]any `json:"fields"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Fields == nil {
		req.Fields = map[string]any{}
	}
	var chainRaw, schemaRaw []byte
	var slaDays sql.NullInt64
	if err := a.db.QueryRow(`SELECT approval_chain_json, form_schema_json, sla_days FROM request_types WHERE id = ? AND is_active = TRUE`, req.RequestTypeID).Scan(&chainRaw, &schemaRaw, &slaDays); err != nil {
		writeError(w, http.StatusBadRequest, "request type not found or inactive")
		return
	}
	if err := validateRequestFields(schemaRaw, req.Fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	steps, err := a.resolveApprovers(currentUserID(r), chainRaw, req.Fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	status := "in_approval"
	if len(steps) == 0 {
		status = "in_progress"
	}
	var dueAt any
	if slaDays.Valid && slaDays.Int64 > 0 {
		dueAt = time.Now().AddDate(0, 0, int(slaDays.Int64))
	}
	res, err := tx.Exec(`INSERT INTO requests (ticket_number, request_type_id, user_id, overall_status, current_stage, due_at) VALUES (?, ?, ?, ?, 0, ?)`,
		ticketNumber(), req.RequestTypeID, currentUserID(r), status, dueAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	requestID, _ := res.LastInsertId()
	for key, value := range req.Fields {
		raw, _ := rawJSON(value)
		if _, err := tx.Exec(`INSERT INTO request_fields (request_id, field_key, field_value) VALUES (?, ?, ?)`, requestID, key, raw); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	for _, step := range steps {
		if _, err := tx.Exec(`INSERT INTO approvals (request_id, approver_id, level, due_at) VALUES (?, ?, ?, ?)`, requestID, step.UserID, step.Level, approvalDueAt(step.Level, len(steps), slaDays)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if _, err := tx.Exec(`INSERT INTO request_status_log (request_id, stage_number, status_text, updated_by_user_id, note) VALUES (?, 0, ?, ?, ?)`,
		requestID, "Pengajuan dibuat", currentUserID(r), "Menunggu proses berikutnya"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(steps) > 0 {
		a.notify(steps[0].UserID, requestID, "approval_needed", "Ada pengajuan yang membutuhkan approval Anda")
	} else {
		a.notifyPICs(req.RequestTypeID, requestID, "request_assigned", "Ada pengajuan baru untuk form yang Anda pegang")
	}
	a.audit(currentUserID(r), "create_request", "request", requestID, req)
	writeJSON(w, http.StatusCreated, map[string]any{"id": requestID, "status": status})
}

func validateRequestFields(raw []byte, values map[string]any) error {
	var schema formSchema
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	for _, field := range schema.Fields {
		if field.Key == "" {
			continue
		}
		value, exists := values[field.Key]
		if field.Required && (!exists || emptyFieldValue(value)) {
			label := field.Label
			if label == "" {
				label = field.Key
			}
			return errors.New(label + " wajib diisi")
		}
		if !exists || emptyFieldValue(value) {
			continue
		}
		switch field.Type {
		case "number":
			if _, ok := value.(float64); !ok {
				if _, err := strconv.ParseFloat(asString(value), 64); err != nil {
					return errors.New(field.Key + " harus berupa angka")
				}
			}
		case "select":
			if len(field.Options) > 0 && !stringInSlice(asString(value), field.Options) {
				return errors.New(field.Key + " tidak sesuai opsi yang tersedia")
			}
		}
	}
	return nil
}

func emptyFieldValue(value any) bool {
	if value == nil {
		return true
	}
	if s, ok := value.(string); ok {
		return s == ""
	}
	return false
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func stringInSlice(value string, options []string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func approvalDueAt(level, total int, slaDays sql.NullInt64) any {
	days := level
	if slaDays.Valid && slaDays.Int64 > 0 && total > 0 {
		perStep := int(slaDays.Int64) / total
		if perStep < 1 {
			perStep = 1
		}
		days = perStep * level
	}
	return time.Now().AddDate(0, 0, days)
}

func (a *App) resolveApprovers(requesterID int64, raw []byte, fields map[string]any) ([]approvalStep, error) {
	var steps []approvalStep
	if len(raw) == 0 {
		return steps, nil
	}
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil, err
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].Level < steps[j].Level })
	resolved := make([]approvalStep, 0, len(steps))
	for i := range steps {
		if steps[i].MinAmount > 0 {
			fieldKey := steps[i].FieldKey
			if fieldKey == "" {
				fieldKey = "amount"
			}
			if numberValue(fields[fieldKey]) < steps[i].MinAmount {
				continue
			}
		}
		switch steps[i].Type {
		case "manager":
			var managerID sql.NullInt64
			if err := a.db.QueryRow(`SELECT manager_id FROM users WHERE id = ?`, requesterID).Scan(&managerID); err != nil {
				return nil, err
			}
			if !managerID.Valid {
				continue
			}
			steps[i].UserID = managerID.Int64
		case "user":
			if steps[i].UserID == 0 {
				return nil, errors.New("user approval step requires user_id")
			}
		case "role":
			if steps[i].Role == "" {
				return nil, errors.New("role approval step requires role")
			}
			query := `SELECT u.id FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id WHERE r.name = ? AND u.status = 'active'`
			args := []any{steps[i].Role}
			if steps[i].DepartmentID > 0 {
				query += ` AND u.department_id = ?`
				args = append(args, steps[i].DepartmentID)
			}
			if steps[i].SiteLocationID > 0 {
				query += ` AND u.site_location_id = ?`
				args = append(args, steps[i].SiteLocationID)
			}
			query += ` ORDER BY u.id LIMIT 1`
			if err := a.db.QueryRow(query, args...).Scan(&steps[i].UserID); err != nil {
				return nil, errors.New("no active approver found for role " + steps[i].Role)
			}
		default:
			return nil, errors.New("unknown approval type: " + steps[i].Type)
		}
		resolved = append(resolved, steps[i])
	}
	return resolved, nil
}

func numberValue(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	default:
		return 0
	}
}

func (a *App) myRequests(w http.ResponseWriter, r *http.Request) {
	a.listRequests(w, r, `WHERE req.user_id = ? AND req.deleted_at IS NULL`, []any{currentUserID(r)})
}

func (a *App) assignedRequests(w http.ResponseWriter, r *http.Request) {
	a.listRequests(w, r, `JOIN request_type_pic rtp ON rtp.request_type_id = req.request_type_id WHERE rtp.user_id = ? AND req.deleted_at IS NULL`, []any{currentUserID(r)})
}

func (a *App) deletedRequests(w http.ResponseWriter, r *http.Request) {
	a.listRequests(w, r, `WHERE req.deleted_at IS NOT NULL`, nil)
}

func (a *App) listRequests(w http.ResponseWriter, r *http.Request, where string, args []any) {
	args = append([]any{}, args...)
	query := `
		SELECT req.id, req.ticket_number, req.request_type_id, rt.name, req.user_id, req.overall_status, req.current_stage, req.due_at,
		       (req.due_at IS NOT NULL AND req.due_at < NOW() AND req.overall_status NOT IN ('done','rejected')) is_overdue, req.created_at, req.deleted_at,
		       (SELECT COUNT(*) FROM approvals ap WHERE ap.request_id = req.id) approval_total,
		       (SELECT COUNT(*) FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'approve') approved_total,
		       (SELECT u.name FROM approvals ap JOIN users u ON u.id = ap.approver_id WHERE ap.request_id = req.id AND ap.action = 'approve' ORDER BY ap.acted_at DESC, ap.level DESC LIMIT 1) last_approver_name,
		       (SELECT ap.acted_at FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'approve' ORDER BY ap.acted_at DESC, ap.level DESC LIMIT 1) last_approved_at,
		       (SELECT u.name FROM approvals ap JOIN users u ON u.id = ap.approver_id WHERE ap.request_id = req.id AND ap.action = 'pending' ORDER BY ap.level ASC LIMIT 1) next_approver_name,
		       (SELECT ap.level FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'pending' ORDER BY ap.level ASC LIMIT 1) next_approval_level,
		       (SELECT ap.created_at FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'pending' ORDER BY ap.level ASC LIMIT 1) next_approval_created_at,
		       (SELECT ap.due_at FROM approvals ap WHERE ap.request_id = req.id AND ap.action = 'pending' ORDER BY ap.level ASC LIMIT 1) next_approval_due_at
		FROM requests req
		JOIN request_types rt ON rt.id = req.request_type_id ` + where + `
		`
	countQuery := `SELECT COUNT(*) FROM requests req JOIN request_types rt ON rt.id = req.request_type_id ` + where
	if status := r.URL.Query().Get("status"); status != "" {
		query += ` AND req.overall_status = ?`
		countQuery += ` AND req.overall_status = ?`
		args = append(args, status)
	}
	if typeID := r.URL.Query().Get("request_type_id"); typeID != "" {
		query += ` AND req.request_type_id = ?`
		countQuery += ` AND req.request_type_id = ?`
		args = append(args, typeID)
	}
	if search := r.URL.Query().Get("search"); search != "" {
		query += ` AND req.ticket_number LIKE ?`
		countQuery += ` AND req.ticket_number LIKE ?`
		args = append(args, "%"+search+"%")
	}
	sortDir := "DESC"
	if r.URL.Query().Get("sort") == "oldest" {
		sortDir = "ASC"
	}
	limit := clampInt(queryInt(r, "limit", 20), 1, 100)
	page := clampInt(queryInt(r, "page", 1), 1, 100000)
	var total int64
	if err := a.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	query += ` ORDER BY req.id ` + sortDir + ` LIMIT ? OFFSET ?`
	args = append(args, limit, (page-1)*limit)
	rows, err := a.db.Query(query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []RequestSummary
	for rows.Next() {
		var s RequestSummary
		var lastApproverName, nextApproverName sql.NullString
		var lastApprovedAt, nextApprovalCreatedAt, nextApprovalDueAt, deletedAt sql.NullTime
		var nextApprovalLevel sql.NullInt64
		if err := rows.Scan(
			&s.ID, &s.TicketNumber, &s.RequestTypeID, &s.RequestTypeName, &s.UserID, &s.OverallStatus, &s.CurrentStage, &s.DueAt,
			&s.IsOverdue, &s.CreatedAt, &deletedAt, &s.ApprovalTotal, &s.ApprovedTotal, &lastApproverName, &lastApprovedAt,
			&nextApproverName, &nextApprovalLevel, &nextApprovalCreatedAt, &nextApprovalDueAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if deletedAt.Valid {
			s.DeletedAt = &deletedAt.Time
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
		s.fillLabels()
		out = append(out, s)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out,
		"page":  page,
		"limit": limit,
		"total": total,
	})
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (a *App) requestDetail(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	allowed, err := a.canAccessRequest(currentUserID(r), id)
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "cannot access request")
		return
	}
	detail, err := a.buildRequestDetail(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	actions, err := a.requestActions(currentUserID(r), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	detail["actions"] = actions
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) requestTimeline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	allowed, err := a.canAccessRequest(currentUserID(r), id)
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "cannot access request")
		return
	}
	events, err := a.buildTimeline(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (a *App) exportRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_ID", "invalid id")
		return
	}
	allowed, err := a.canAccessRequest(currentUserID(r), id)
	if err != nil || !allowed {
		writeErrorCode(w, http.StatusForbidden, "REQUEST_FORBIDDEN", "cannot access request")
		return
	}
	detail, err := a.buildRequestDetail(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exported_at": time.Now(),
		"detail":      detail,
	})
}

func (a *App) requestActions(userID, requestID int64) (map[string]bool, error) {
	var ownerID int64
	var status string
	var deletedAt sql.NullTime
	if err := a.db.QueryRow(`SELECT user_id, overall_status, deleted_at FROM requests WHERE id = ?`, requestID).Scan(&ownerID, &status, &deletedAt); err != nil {
		return nil, err
	}
	canViewAll, err := a.hasPermission(userID, "request.view_all")
	if err != nil {
		return nil, err
	}
	canApprovePerm, err := a.hasPermission(userID, "request.approve")
	if err != nil {
		return nil, err
	}
	canProgressPerm, err := a.hasPermission(userID, "request.update_progress")
	if err != nil {
		return nil, err
	}
	canResultPerm, err := a.hasPermission(userID, "request.give_result")
	if err != nil {
		return nil, err
	}
	pendingApproval := false
	var pendingApprovalFlag int
	if err := a.db.QueryRow(`SELECT 1 FROM approvals WHERE request_id = ? AND approver_id = ? AND action = 'pending' LIMIT 1`, requestID, userID).Scan(&pendingApprovalFlag); err == nil {
		pendingApproval = true
	}
	isPIC, err := a.isPICForRequest(userID, requestID)
	if err != nil {
		return nil, err
	}
	active := !deletedAt.Valid
	return map[string]bool{
		"can_approve":         active && status == "in_approval" && canApprovePerm && pendingApproval,
		"can_update_progress": active && status == "in_progress" && canProgressPerm && isPIC,
		"can_move_pic_stage":  active && status == "in_progress" && canProgressPerm && isPIC,
		"can_give_result":     active && status == "in_progress" && canResultPerm && isPIC,
		"can_delete":          active && (canViewAll || ownerID == userID),
		"can_restore":         deletedAt.Valid && canViewAll,
		"can_comment":         active,
		"can_export":          true,
	}, nil
}

func (a *App) deleteRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	userID := currentUserID(r)
	canDelete, err := a.canDeleteRequest(userID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canDelete {
		writeError(w, http.StatusForbidden, "cannot delete this request")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &req)
	res, err := a.db.Exec(`UPDATE requests SET deleted_at = NOW() WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	a.audit(userID, "soft_delete_request", "request", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) restoreRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &req)
	res, err := a.db.Exec(`UPDATE requests SET deleted_at = NULL WHERE id = ? AND deleted_at IS NOT NULL`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "deleted request not found")
		return
	}
	a.audit(currentUserID(r), "restore_request", "request", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) canDeleteRequest(userID, requestID int64) (bool, error) {
	if ok, err := a.hasPermission(userID, "request.view_all"); ok || err != nil {
		return ok, err
	}
	var exists int
	err := a.db.QueryRow(`SELECT 1 FROM requests WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, requestID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (a *App) requestStatus(requestID int64) (string, error) {
	var status string
	err := a.db.QueryRow(`SELECT overall_status FROM requests WHERE id = ? AND deleted_at IS NULL`, requestID).Scan(&status)
	return status, err
}

func (a *App) approveRequest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Action != "approve" && req.Action != "reject" {
		writeError(w, http.StatusBadRequest, "action must be approve or reject")
		return
	}
	status, err := a.requestStatus(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != "in_approval" {
		writeErrorCode(w, http.StatusBadRequest, "REQUEST_NOT_APPROVABLE", "request is not waiting for approval")
		return
	}
	var level int
	var requesterID, requestTypeID int64
	err = a.db.QueryRow(`
		SELECT a.level, req.user_id, req.request_type_id
		FROM approvals a JOIN requests req ON req.id = a.request_id
		WHERE a.request_id = ? AND a.approver_id = ? AND a.action = 'pending' AND req.deleted_at IS NULL
		ORDER BY a.level LIMIT 1`, id, currentUserID(r)).Scan(&level, &requesterID, &requestTypeID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusForbidden, "no pending approval for this user")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tx, _ := a.db.Begin()
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE approvals SET action = ?, note = ?, acted_at = ? WHERE request_id = ? AND approver_id = ? AND level = ?`,
		req.Action, req.Note, time.Now(), id, currentUserID(r), level); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Action == "reject" {
		if _, err := tx.Exec(`UPDATE requests SET overall_status = 'rejected', current_stage = ? WHERE id = ?`, level, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, _ = tx.Exec(`INSERT INTO request_status_log (request_id, stage_number, status_text, updated_by_user_id, note) VALUES (?, ?, 'Ditolak', ?, ?)`, id, level, currentUserID(r), req.Note)
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.notify(requesterID, id, "status_update", "Pengajuan Anda ditolak")
		a.audit(currentUserID(r), "reject_request", "request", id, req)
		writeJSON(w, http.StatusOK, map[string]any{"status": "rejected"})
		return
	}
	var nextApprover int64
	err = tx.QueryRow(`SELECT approver_id FROM approvals WHERE request_id = ? AND action = 'pending' ORDER BY level LIMIT 1`, id).Scan(&nextApprover)
	if err == sql.ErrNoRows {
		if _, err := tx.Exec(`UPDATE requests SET overall_status = 'in_progress', current_stage = ? WHERE id = ?`, level, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, _ = tx.Exec(`INSERT INTO request_status_log (request_id, stage_number, status_text, updated_by_user_id, note) VALUES (?, ?, 'Semua approval selesai', ?, ?)`, id, level, currentUserID(r), req.Note)
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.notify(requesterID, id, "status_update", "Pengajuan Anda masuk tahap pengerjaan PIC")
		a.notifyPICs(requestTypeID, id, "request_assigned", "Ada pengajuan baru untuk ditangani")
		a.audit(currentUserID(r), "approve_request", "request", id, req)
		writeJSON(w, http.StatusOK, map[string]any{"status": "in_progress"})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE requests SET current_stage = ? WHERE id = ?`, level, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.notify(nextApprover, id, "approval_needed", "Ada pengajuan yang membutuhkan approval Anda")
	a.audit(currentUserID(r), "approve_request", "request", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"status": "in_approval", "next_approver_id": nextApprover})
}

func (a *App) addStatusLog(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r)
	if ok, _ := a.isPICForRequest(currentUserID(r), id); !ok {
		writeError(w, http.StatusForbidden, "only assigned PIC can update progress")
		return
	}
	status, err := a.requestStatus(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != "in_progress" {
		writeErrorCode(w, http.StatusBadRequest, "REQUEST_NOT_IN_PROGRESS", "request must be in_progress before PIC can update it")
		return
	}
	var req struct {
		StageNumber int    `json:"stage_number"`
		StatusText  string `json:"status_text"`
		Note        string `json:"note"`
		MarkDone    bool   `json:"mark_done"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err = a.db.Exec(`INSERT INTO request_status_log (request_id, stage_number, status_text, updated_by_user_id, note) VALUES (?, ?, ?, ?, ?)`,
		id, req.StageNumber, req.StatusText, currentUserID(r), req.Note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	nextStatus := "in_progress"
	if req.MarkDone {
		nextStatus = "done"
	}
	_, _ = a.db.Exec(`UPDATE requests SET overall_status = ?, current_stage = ? WHERE id = ?`, nextStatus, req.StageNumber, id)
	requesterID := a.requestOwner(id)
	a.notify(requesterID, id, "status_update", "Progress pengajuan Anda diperbarui")
	a.audit(currentUserID(r), "update_progress", "request", id, req)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (a *App) movePICStage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_ID", "invalid id")
		return
	}
	if ok, _ := a.isPICForRequest(currentUserID(r), id); !ok {
		writeErrorCode(w, http.StatusForbidden, "PIC_FORBIDDEN", "only assigned PIC can move stage")
		return
	}
	status, err := a.requestStatus(id)
	if err == sql.ErrNoRows {
		writeErrorCode(w, http.StatusNotFound, "REQUEST_NOT_FOUND", "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != "in_progress" {
		writeErrorCode(w, http.StatusBadRequest, "REQUEST_NOT_IN_PROGRESS", "request must be in_progress before PIC stage can move")
		return
	}
	var req struct {
		StageNumber int    `json:"stage_number"`
		Note        string `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	if req.StageNumber < 1 {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_STAGE", "stage_number must be greater than zero")
		return
	}
	var exists int
	if err := a.db.QueryRow(`
		SELECT 1
		FROM requests req
		JOIN request_type_pic rtp ON rtp.request_type_id = req.request_type_id
		WHERE req.id = ? AND rtp.stage_number = ?
		LIMIT 1`, id, req.StageNumber).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusBadRequest, "PIC_STAGE_NOT_FOUND", "PIC stage not found for this request type")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = a.db.Exec(`UPDATE requests SET current_stage = ? WHERE id = ? AND deleted_at IS NULL`, req.StageNumber, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = a.db.Exec(`INSERT INTO request_status_log (request_id, stage_number, status_text, updated_by_user_id, note) VALUES (?, ?, 'Pindah stage PIC', ?, ?)`,
		id, req.StageNumber, currentUserID(r), req.Note)
	a.audit(currentUserID(r), "move_pic_stage", "request", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stage_number": req.StageNumber})
}

func (a *App) addComment(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r)
	if ok, _ := a.canAccessRequest(currentUserID(r), id); !ok {
		writeError(w, http.StatusForbidden, "cannot access request")
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.db.Exec(`INSERT INTO request_comments (request_id, user_id, message) VALUES (?, ?, ?)`, id, currentUserID(r), req.Message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	commentID, _ := res.LastInsertId()
	a.notifyConversationUsers(id, currentUserID(r), "new_comment", "Ada komentar baru pada pengajuan")
	a.audit(currentUserID(r), "add_comment", "request_comment", commentID, map[string]any{"request_id": id})
	writeJSON(w, http.StatusCreated, map[string]any{"id": commentID})
}

func (a *App) updateComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.db.Exec(`UPDATE request_comments SET message = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, req.Message, id, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusForbidden, "cannot update this comment")
		return
	}
	a.audit(currentUserID(r), "update_comment", "request_comment", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	res, err := a.db.Exec(`UPDATE request_comments SET deleted_at = NOW() WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, id, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusForbidden, "cannot delete this comment")
		return
	}
	a.audit(currentUserID(r), "soft_delete_comment", "request_comment", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) giveResult(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r)
	if ok, _ := a.isPICForRequest(currentUserID(r), id); !ok {
		writeError(w, http.StatusForbidden, "only assigned PIC can give result")
		return
	}
	status, err := a.requestStatus(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != "in_progress" {
		writeErrorCode(w, http.StatusBadRequest, "REQUEST_NOT_IN_PROGRESS", "request must be in_progress before result can be given")
		return
	}
	var req struct {
		ResultText string `json:"result_text"`
		FileURL    string `json:"file_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.db.Exec(`INSERT INTO request_results (request_id, result_text, given_by_user_id) VALUES (?, ?, ?)`, id, req.ResultText, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resultID, _ := res.LastInsertId()
	if req.FileURL != "" {
		_, _ = a.db.Exec(`INSERT INTO attachments (request_id, source_type, source_id, file_url, file_name, uploaded_by_user_id) VALUES (?, 'result', ?, ?, ?, ?)`, id, resultID, req.FileURL, req.FileURL, currentUserID(r))
	}
	_, _ = a.db.Exec(`UPDATE requests SET overall_status = 'done' WHERE id = ?`, id)
	requesterID := a.requestOwner(id)
	a.notify(requesterID, id, "result_given", "Hasil pengajuan sudah diberikan")
	a.audit(currentUserID(r), "give_result", "request_result", resultID, map[string]any{"request_id": id})
	writeJSON(w, http.StatusCreated, map[string]any{"id": resultID})
}
