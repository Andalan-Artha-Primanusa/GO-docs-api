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

func (a *App) updateMyProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string  `json:"name"`
		Phone *string `json:"phone"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	if req.Name == "" {
		writeErrorCode(w, http.StatusBadRequest, "NAME_REQUIRED", "name is required")
		return
	}
	if _, err := a.db.Exec(`UPDATE users SET name = ?, phone = ? WHERE id = ?`, req.Name, req.Phone, currentUserID(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "update_my_profile", "user", currentUserID(r), req)
	user, err := a.getUser(currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (a *App) myPreferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := a.ensurePreferences(currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (a *App) updateMyPreferences(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NotifyInApp    bool   `json:"notify_in_app"`
		NotifyEmail    bool   `json:"notify_email"`
		CompactSidebar bool   `json:"compact_sidebar"`
		Theme          string `json:"theme"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	if req.Theme == "" {
		req.Theme = "system"
	}
	if req.Theme != "system" && req.Theme != "light" && req.Theme != "dark" {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_THEME", "theme must be system, light, or dark")
		return
	}
	_, err := a.db.Exec(`
		INSERT INTO user_preferences (user_id, notify_in_app, notify_email, compact_sidebar, theme)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE notify_in_app = VALUES(notify_in_app), notify_email = VALUES(notify_email), compact_sidebar = VALUES(compact_sidebar), theme = VALUES(theme)`,
		currentUserID(r), req.NotifyInApp, req.NotifyEmail, req.CompactSidebar, req.Theme)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(currentUserID(r), "update_my_preferences", "user", currentUserID(r), req)
	prefs, err := a.ensurePreferences(currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (a *App) ensurePreferences(userID int64) (map[string]any, error) {
	_, _ = a.db.Exec(`INSERT IGNORE INTO user_preferences (user_id) VALUES (?)`, userID)
	var notifyInApp, notifyEmail, compactSidebar bool
	var theme string
	err := a.db.QueryRow(`SELECT notify_in_app, notify_email, compact_sidebar, theme FROM user_preferences WHERE user_id = ?`, userID).
		Scan(&notifyInApp, &notifyEmail, &compactSidebar, &theme)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"notify_in_app":   notifyInApp,
		"notify_email":    notifyEmail,
		"compact_sidebar": compactSidebar,
		"theme":           theme,
	}, nil
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
