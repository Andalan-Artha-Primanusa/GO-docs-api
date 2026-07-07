package database

import (
	"database/sql"

	"rbac-request-engine/internal/security"
)

func Seed(db *sql.DB) error {
	if _, err := db.Exec(`INSERT IGNORE INTO departments (id, name) VALUES (1,'IT'),(2,'Finance'),(3,'HR'),(4,'GA'),(5,'General')`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT IGNORE INTO site_locations (id, name, address) VALUES
		(1,'Head Office','Kantor pusat'),
		(2,'Site Jakarta','Jakarta'),
		(3,'Site Bandung','Bandung'),
		(4,'Site Surabaya','Surabaya')`); err != nil {
		return err
	}

	permissions := []string{
		"user.manage", "role.manage", "request_type.manage", "request_type.assign_pic",
		"request.create", "request.approve", "request.update_progress", "request.give_result",
		"request.view_all",
	}
	for _, p := range permissions {
		if _, err := db.Exec(`INSERT IGNORE INTO permissions (code, description) VALUES (?, ?)`, p, p); err != nil {
			return err
		}
	}

	roles := map[string][]string{
		"super_admin": {"user.manage", "role.manage", "request_type.manage", "request_type.assign_pic", "request.create", "request.approve", "request.update_progress", "request.give_result", "request.view_all"},
		"hr":          {"user.manage", "request.create", "request.approve"},
		"manager":     {"request.create", "request.approve"},
		"finance":     {"request.create", "request.approve"},
		"staff":       {"request.create"},
	}
	for role, perms := range roles {
		if _, err := db.Exec(`INSERT IGNORE INTO roles (name, description) VALUES (?, ?)`, role, role); err != nil {
			return err
		}
		for _, perm := range perms {
			if _, err := db.Exec(`
				INSERT IGNORE INTO role_permissions (role_id, permission_id)
				SELECT r.id, p.id FROM roles r, permissions p WHERE r.name = ? AND p.code = ?`, role, perm); err != nil {
				return err
			}
		}
	}

	pass, err := security.HashPassword("password123")
	if err != nil {
		return err
	}
	users := []struct {
		name   string
		email  string
		deptID int
		siteID int
		role   string
	}{
		{"Super Admin", "superadmin@example.com", 1, 1, "super_admin"},
		{"HR Admin", "hr@example.com", 3, 1, "hr"},
		{"Manager", "manager@example.com", 5, 2, "manager"},
		{"Staff", "staff@example.com", 5, 2, "staff"},
	}
	for _, u := range users {
		if _, err := db.Exec(`INSERT IGNORE INTO users (name, email, password_hash, department_id, site_location_id, status) VALUES (?, ?, ?, ?, ?, 'active')`,
			u.name, u.email, pass, u.deptID, u.siteID); err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE users SET department_id = COALESCE(department_id, ?), site_location_id = COALESCE(site_location_id, ?) WHERE email = ?`,
			u.deptID, u.siteID, u.email); err != nil {
			return err
		}
		if _, err := db.Exec(`
			INSERT IGNORE INTO user_roles (user_id, role_id)
			SELECT u.id, r.id FROM users u, roles r WHERE u.email = ? AND r.name = ?`, u.email, u.role); err != nil {
			return err
		}
	}
	_, err = db.Exec(`UPDATE users staff JOIN users mgr SET staff.manager_id = mgr.id WHERE staff.email = 'staff@example.com' AND mgr.email = 'manager@example.com'`)
	return err
}
