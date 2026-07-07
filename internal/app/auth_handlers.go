package app

import (
	"database/sql"
	"net/http"
	"time"

	"rbac-request-engine/internal/security"
)

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		Email          string `json:"email"`
		Password       string `json:"password"`
		DepartmentID   *int64 `json:"department_id"`
		SiteLocationID *int64 `json:"site_location_id"`
		Phone          string `json:"phone"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "name, email, and password are required")
		return
	}
	hash, err := security.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	res, err := a.db.Exec(`INSERT INTO users (name, email, password_hash, department_id, site_location_id, phone, status) VALUES (?, ?, ?, ?, ?, ?, 'pending')`,
		req.Name, req.Email, hash, req.DepartmentID, req.SiteLocationID, req.Phone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	a.audit(id, "register", "user", id, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "pending"})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var id int64
	var hash, status string
	err := a.db.QueryRow(`SELECT id, password_hash, status FROM users WHERE email = ?`, req.Email).Scan(&id, &hash, &status)
	if err == sql.ErrNoRows || !security.CheckPassword(req.Password, hash) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != "active" {
		writeError(w, http.StatusForbidden, "account is not active")
		return
	}
	token, err := security.SignToken(id, a.cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "token_type": "Bearer", "expires_in": 86400})
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	user, err := a.getUser(currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (a *App) getUser(id int64) (User, error) {
	var u User
	err := a.db.QueryRow(`
		SELECT u.id, u.name, u.email, u.department_id, d.name, u.site_location_id, sl.name,
		       u.manager_id, u.phone, u.status, u.created_at
		FROM users u
		LEFT JOIN departments d ON d.id = u.department_id
		LEFT JOIN site_locations sl ON sl.id = u.site_location_id
		WHERE u.id = ?`, id).
		Scan(&u.ID, &u.Name, &u.Email, &u.DepartmentID, &u.Department, &u.SiteLocationID, &u.SiteLocation, &u.ManagerID, &u.Phone, &u.Status, &u.CreatedAt)
	if err != nil {
		return u, err
	}
	rows, err := a.db.Query(`SELECT r.name FROM roles r JOIN user_roles ur ON ur.role_id = r.id WHERE ur.user_id = ?`, id)
	if err != nil {
		return u, err
	}
	u.Roles, err = scanStrings(rows)
	if err != nil {
		return u, err
	}
	rows, err = a.db.Query(`
		SELECT DISTINCT p.code
		FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN user_roles ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = ?`, id)
	if err != nil {
		return u, err
	}
	u.Permissions, err = scanStrings(rows)
	return u, err
}
