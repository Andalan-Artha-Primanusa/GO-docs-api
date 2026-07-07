package app

import (
	"encoding/json"
	"net/http"
)

func (a *App) listRequestTypes(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`
		SELECT rt.id, rt.name, rt.department_owner_id, rt.form_schema_json, rt.approval_chain_json, rt.is_active, rt.sla_days, rt.created_by, rt.created_at,
		       COALESCE(JSON_ARRAYAGG(JSON_OBJECT('id', u.id, 'name', u.name, 'email', u.email, 'stage_number', rtp.stage_number)), JSON_ARRAY())
		FROM request_types rt
		LEFT JOIN request_type_pic rtp ON rtp.request_type_id = rt.id
		LEFT JOIN users u ON u.id = rtp.user_id
		GROUP BY rt.id
		ORDER BY rt.name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, createdBy int64
		var name string
		var deptID, slaDays *int64
		var formRaw, chainRaw, picsRaw []byte
		var active bool
		var createdAt any
		if err := rows.Scan(&id, &name, &deptID, &formRaw, &chainRaw, &active, &slaDays, &createdBy, &createdAt, &picsRaw); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var form, chain, pics any
		_ = json.Unmarshal(formRaw, &form)
		_ = json.Unmarshal(chainRaw, &chain)
		_ = json.Unmarshal(picsRaw, &pics)
		out = append(out, map[string]any{
			"id": id, "name": name, "department_owner_id": deptID, "form_schema": form,
			"approval_chain": chain, "is_active": active, "sla_days": slaDays, "created_by": createdBy, "created_at": createdAt, "pics": pics,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) createRequestType(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name              string `json:"name"`
		DepartmentOwnerID *int64 `json:"department_owner_id"`
		FormSchema        any    `json:"form_schema"`
		ApprovalChain     any    `json:"approval_chain"`
		IsActive          *bool  `json:"is_active"`
		SLADays           *int   `json:"sla_days"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	form, err := rawJSON(req.FormSchema)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid form_schema")
		return
	}
	chain, err := rawJSON(req.ApprovalChain)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid approval_chain")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	res, err := a.db.Exec(`INSERT INTO request_types (name, department_owner_id, form_schema_json, approval_chain_json, is_active, sla_days, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.DepartmentOwnerID, form, chain, active, req.SLADays, currentUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	a.audit(currentUserID(r), "create_request_type", "request_type", id, req)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) updateRequestType(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name              string `json:"name"`
		DepartmentOwnerID *int64 `json:"department_owner_id"`
		FormSchema        any    `json:"form_schema"`
		ApprovalChain     any    `json:"approval_chain"`
		IsActive          bool   `json:"is_active"`
		SLADays           *int   `json:"sla_days"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	form, _ := rawJSON(req.FormSchema)
	chain, _ := rawJSON(req.ApprovalChain)
	_, err = a.db.Exec(`UPDATE request_types SET name = ?, department_owner_id = ?, form_schema_json = ?, approval_chain_json = ?, is_active = ?, sla_days = ? WHERE id = ?`,
		req.Name, req.DepartmentOwnerID, form, chain, req.IsActive, req.SLADays, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.audit(currentUserID(r), "update_request_type", "request_type", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) assignPICs(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		UserIDs     []int64 `json:"user_ids"`
		Assignments []struct {
			StageNumber int     `json:"stage_number"`
			UserIDs     []int64 `json:"user_ids"`
		} `json:"assignments"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM request_type_pic WHERE request_type_id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(req.Assignments) == 0 && len(req.UserIDs) > 0 {
		req.Assignments = append(req.Assignments, struct {
			StageNumber int     `json:"stage_number"`
			UserIDs     []int64 `json:"user_ids"`
		}{StageNumber: 1, UserIDs: req.UserIDs})
	}
	for _, assignment := range req.Assignments {
		stageNumber := assignment.StageNumber
		if stageNumber <= 0 {
			stageNumber = 1
		}
		for _, userID := range assignment.UserIDs {
			if _, err := tx.Exec(`INSERT INTO request_type_pic (request_type_id, user_id, stage_number, assigned_by) VALUES (?, ?, ?, ?)`, id, userID, stageNumber, currentUserID(r)); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "assign_pics", "request_type", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
