package app

import (
	"net/http"

	"rbac-request-engine/internal/security"
)

func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`
		SELECT u.id, u.name, u.email, u.department_id, d.name, u.site_location_id, sl.name,
		       u.manager_id, u.phone, u.status, u.created_at
		FROM users u
		LEFT JOIN departments d ON d.id = u.department_id
		LEFT JOIN site_locations sl ON sl.id = u.site_location_id
		ORDER BY u.id DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.DepartmentID, &u.Department, &u.SiteLocationID, &u.SiteLocation, &u.ManagerID, &u.Phone, &u.Status, &u.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, u)
	}
	for i := range users {
		if fullUser, err := a.getUser(users[i].ID); err == nil {
			users[i].Roles = fullUser.Roles
			users[i].Permissions = fullUser.Permissions
		}
	}
	writeJSON(w, http.StatusOK, users)
}

func (a *App) listDepartments(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, name, created_at FROM departments ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var name string
		var createdAt any
		if err := rows.Scan(&id, &name, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) listSiteLocations(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, name, address, created_at FROM site_locations ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var name string
		var address *string
		var createdAt any
		if err := rows.Scan(&id, &name, &address, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "address": address, "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) activateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, err = a.db.Exec(`UPDATE users SET status = 'active' WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.notify(id, 0, "account_verified", "Akun Anda sudah aktif")
	a.audit(currentUserID(r), "activate_user", "user", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) setUserRoles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		RoleIDs []int64 `json:"role_ids"`
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
	if _, err := tx.Exec(`DELETE FROM user_roles WHERE user_id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, roleID := range req.RoleIDs {
		if _, err := tx.Exec(`INSERT IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)`, id, roleID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "set_user_roles", "user", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) listRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, name, description FROM roles ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var name string
		var desc *string
		if err := rows.Scan(&id, &name, &desc); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "description": desc})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) createRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string  `json:"name"`
		Description   *string `json:"description"`
		PermissionIDs []int64 `json:"permission_ids"`
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
	res, err := tx.Exec(`INSERT INTO roles (name, description) VALUES (?, ?)`, req.Name, req.Description)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	for _, permissionID := range req.PermissionIDs {
		if _, err := tx.Exec(`INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)`, id, permissionID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "create_role", "role", id, req)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) listPermissions(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, code, description FROM permissions ORDER BY code`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var code string
		var desc *string
		if err := rows.Scan(&id, &code, &desc); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, map[string]any{"id": id, "code": code, "description": desc})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string  `json:"name"`
		Email          string  `json:"email"`
		Password       string  `json:"password"`
		DepartmentID   *int64  `json:"department_id"`
		SiteLocationID *int64  `json:"site_location_id"`
		ManagerID      *int64  `json:"manager_id"`
		Phone          *string `json:"phone"`
		Status         string  `json:"status"`
		RoleIDs        []int64 `json:"role_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "name and email are required")
		return
	}
	if req.Password == "" {
		req.Password = "password123"
	}
	if req.Status == "" {
		req.Status = "active"
	}
	hash, err := security.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO users (name, email, password_hash, department_id, site_location_id, manager_id, phone, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Email, hash, req.DepartmentID, req.SiteLocationID, req.ManagerID, req.Phone, req.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	for _, roleID := range req.RoleIDs {
		if _, err := tx.Exec(`INSERT IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)`, id, roleID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "create_user", "user", id, req)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name           string  `json:"name"`
		DepartmentID   *int64  `json:"department_id"`
		SiteLocationID *int64  `json:"site_location_id"`
		ManagerID      *int64  `json:"manager_id"`
		Phone          *string `json:"phone"`
		Status         string  `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	_, err = a.db.Exec(`UPDATE users SET name = ?, department_id = ?, site_location_id = ?, manager_id = ?, phone = ?, status = ? WHERE id = ?`,
		req.Name, req.DepartmentID, req.SiteLocationID, req.ManagerID, req.Phone, req.Status, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.audit(currentUserID(r), "update_user", "user", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) disableUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, err = a.db.Exec(`UPDATE users SET status = 'disabled' WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "disable_user", "user", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Password == "" {
		req.Password = "password123"
	}
	hash, err := security.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = a.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "reset_password", "user", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) createDepartment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.db.Exec(`INSERT INTO departments (name) VALUES (?)`, req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	a.audit(currentUserID(r), "create_department", "department", id, req)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) updateDepartment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err = a.db.Exec(`UPDATE departments SET name = ? WHERE id = ?`, req.Name, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.audit(currentUserID(r), "update_department", "department", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) deleteDepartment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, _ = a.db.Exec(`UPDATE users SET department_id = NULL WHERE department_id = ?`, id)
	_, err = a.db.Exec(`DELETE FROM departments WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.audit(currentUserID(r), "delete_department", "department", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) createSiteLocation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string  `json:"name"`
		Address *string `json:"address"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.db.Exec(`INSERT INTO site_locations (name, address) VALUES (?, ?)`, req.Name, req.Address)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	a.audit(currentUserID(r), "create_site_location", "site_location", id, req)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) updateSiteLocation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name    string  `json:"name"`
		Address *string `json:"address"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err = a.db.Exec(`UPDATE site_locations SET name = ?, address = ? WHERE id = ?`, req.Name, req.Address, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.audit(currentUserID(r), "update_site_location", "site_location", id, req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) deleteSiteLocation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, _ = a.db.Exec(`UPDATE users SET site_location_id = NULL WHERE site_location_id = ?`, id)
	_, err = a.db.Exec(`DELETE FROM site_locations WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.audit(currentUserID(r), "delete_site_location", "site_location", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
