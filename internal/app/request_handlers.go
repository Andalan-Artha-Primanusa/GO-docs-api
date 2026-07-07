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

func (a *App) createRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestTypeID int64          `json:"request_type_id"`
		Fields        map[string]any `json:"fields"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var chainRaw []byte
	var slaDays sql.NullInt64
	if err := a.db.QueryRow(`SELECT approval_chain_json, sla_days FROM request_types WHERE id = ? AND is_active = TRUE`, req.RequestTypeID).Scan(&chainRaw, &slaDays); err != nil {
		writeError(w, http.StatusBadRequest, "request type not found or inactive")
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
		if _, err := tx.Exec(`INSERT INTO approvals (request_id, approver_id, level) VALUES (?, ?, ?)`, requestID, step.UserID, step.Level); err != nil {
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
	a.listRequests(w, r, `WHERE req.user_id = ?`, []any{currentUserID(r)})
}

func (a *App) assignedRequests(w http.ResponseWriter, r *http.Request) {
	a.listRequests(w, r, `JOIN request_type_pic rtp ON rtp.request_type_id = req.request_type_id WHERE rtp.user_id = ?`, []any{currentUserID(r)})
}

func (a *App) listRequests(w http.ResponseWriter, r *http.Request, where string, args []any) {
	query := `
		SELECT req.id, req.ticket_number, req.request_type_id, rt.name, req.user_id, req.overall_status, req.current_stage, req.due_at,
		       (req.due_at IS NOT NULL AND req.due_at < NOW() AND req.overall_status NOT IN ('done','rejected')) is_overdue, req.created_at
		FROM requests req
		JOIN request_types rt ON rt.id = req.request_type_id ` + where + `
		`
	if status := r.URL.Query().Get("status"); status != "" {
		query += ` AND req.overall_status = ?`
		args = append(args, status)
	}
	if typeID := r.URL.Query().Get("request_type_id"); typeID != "" {
		query += ` AND req.request_type_id = ?`
		args = append(args, typeID)
	}
	if search := r.URL.Query().Get("search"); search != "" {
		query += ` AND req.ticket_number LIKE ?`
		args = append(args, "%"+search+"%")
	}
	sortDir := "DESC"
	if r.URL.Query().Get("sort") == "oldest" {
		sortDir = "ASC"
	}
	limit := clampInt(queryInt(r, "limit", 20), 1, 100)
	page := clampInt(queryInt(r, "page", 1), 1, 100000)
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
		if err := rows.Scan(&s.ID, &s.TicketNumber, &s.RequestTypeID, &s.RequestTypeName, &s.UserID, &s.OverallStatus, &s.CurrentStage, &s.DueAt, &s.IsOverdue, &s.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, s)
	}
	writeJSON(w, http.StatusOK, out)
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
	writeJSON(w, http.StatusOK, detail)
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
	var level int
	var requesterID, requestTypeID int64
	err = a.db.QueryRow(`
		SELECT a.level, req.user_id, req.request_type_id
		FROM approvals a JOIN requests req ON req.id = a.request_id
		WHERE a.request_id = ? AND a.approver_id = ? AND a.action = 'pending'
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
	_, err := a.db.Exec(`INSERT INTO request_status_log (request_id, stage_number, status_text, updated_by_user_id, note) VALUES (?, ?, ?, ?, ?)`,
		id, req.StageNumber, req.StatusText, currentUserID(r), req.Note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := "in_progress"
	if req.MarkDone {
		status = "done"
	}
	_, _ = a.db.Exec(`UPDATE requests SET overall_status = ?, current_stage = ? WHERE id = ?`, status, req.StageNumber, id)
	requesterID := a.requestOwner(id)
	a.notify(requesterID, id, "status_update", "Progress pengajuan Anda diperbarui")
	a.audit(currentUserID(r), "update_progress", "request", id, req)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
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
	res, err := a.db.Exec(`UPDATE request_comments SET message = ? WHERE id = ? AND user_id = ?`, req.Message, id, currentUserID(r))
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
	res, err := a.db.Exec(`DELETE FROM request_comments WHERE id = ? AND user_id = ?`, id, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusForbidden, "cannot delete this comment")
		return
	}
	a.audit(currentUserID(r), "delete_comment", "request_comment", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) giveResult(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r)
	if ok, _ := a.isPICForRequest(currentUserID(r), id); !ok {
		writeError(w, http.StatusForbidden, "only assigned PIC can give result")
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
		_, _ = a.db.Exec(`INSERT INTO attachments (request_id, source_type, source_id, file_url) VALUES (?, 'result', ?, ?)`, id, resultID, req.FileURL)
	}
	requesterID := a.requestOwner(id)
	a.notify(requesterID, id, "result_given", "Hasil pengajuan sudah diberikan")
	a.audit(currentUserID(r), "give_result", "request_result", resultID, map[string]any{"request_id": id})
	writeJSON(w, http.StatusCreated, map[string]any{"id": resultID})
}
