package app

import (
	"database/sql"
	"net/http"
	"strings"

	"rbac-request-engine/internal/config"
	"rbac-request-engine/internal/security"
)

type App struct {
	db  *sql.DB
	cfg config.Config
}

func New(db *sql.DB, cfg config.Config) *App {
	return &App{db: db, cfg: cfg}
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /api/auth/register", a.register)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.Handle("GET /api/meta/workflow", a.auth(http.HandlerFunc(a.workflowMeta)))
	mux.Handle("GET /api/me", a.auth(http.HandlerFunc(a.me)))
	mux.Handle("PUT /api/me/profile", a.auth(http.HandlerFunc(a.updateMyProfile)))
	mux.Handle("GET /api/me/preferences", a.auth(http.HandlerFunc(a.myPreferences)))
	mux.Handle("PUT /api/me/preferences", a.auth(http.HandlerFunc(a.updateMyPreferences)))
	mux.Handle("GET /api/departments", a.auth(http.HandlerFunc(a.listDepartments)))
	mux.Handle("POST /api/departments", a.require("master.manage", http.HandlerFunc(a.createDepartment)))
	mux.Handle("PUT /api/departments/{id}", a.require("master.manage", http.HandlerFunc(a.updateDepartment)))
	mux.Handle("DELETE /api/departments/{id}", a.require("master.manage", http.HandlerFunc(a.deleteDepartment)))
	mux.Handle("GET /api/site-locations", a.auth(http.HandlerFunc(a.listSiteLocations)))
	mux.Handle("POST /api/site-locations", a.require("master.manage", http.HandlerFunc(a.createSiteLocation)))
	mux.Handle("PUT /api/site-locations/{id}", a.require("master.manage", http.HandlerFunc(a.updateSiteLocation)))
	mux.Handle("DELETE /api/site-locations/{id}", a.require("master.manage", http.HandlerFunc(a.deleteSiteLocation)))

	mux.Handle("GET /api/users", a.require("user.manage", http.HandlerFunc(a.listUsers)))
	mux.Handle("POST /api/users", a.require("user.manage", http.HandlerFunc(a.createUser)))
	mux.Handle("PUT /api/users/{id}", a.require("user.manage", http.HandlerFunc(a.updateUser)))
	mux.Handle("POST /api/users/{id}/activate", a.require("user.manage", http.HandlerFunc(a.activateUser)))
	mux.Handle("POST /api/users/{id}/disable", a.require("user.manage", http.HandlerFunc(a.disableUser)))
	mux.Handle("POST /api/users/{id}/reset-password", a.require("user.manage", http.HandlerFunc(a.resetUserPassword)))
	mux.Handle("POST /api/users/{id}/roles", a.require("user.manage", http.HandlerFunc(a.setUserRoles)))

	mux.Handle("GET /api/roles", a.require("role.manage", http.HandlerFunc(a.listRoles)))
	mux.Handle("POST /api/roles", a.require("role.manage", http.HandlerFunc(a.createRole)))
	mux.Handle("GET /api/permissions", a.require("role.manage", http.HandlerFunc(a.listPermissions)))

	mux.Handle("GET /api/request-types", a.auth(http.HandlerFunc(a.listRequestTypes)))
	mux.Handle("POST /api/request-types", a.require("request_type.manage", http.HandlerFunc(a.createRequestType)))
	mux.Handle("PUT /api/request-types/{id}", a.require("request_type.manage", http.HandlerFunc(a.updateRequestType)))
	mux.Handle("POST /api/request-types/{id}/pics", a.require("request_type.assign_pic", http.HandlerFunc(a.assignPICs)))

	mux.Handle("POST /api/requests", a.require("request.create", http.HandlerFunc(a.createRequest)))
	mux.Handle("GET /api/requests/mine", a.auth(http.HandlerFunc(a.myRequests)))
	mux.Handle("GET /api/requests/assigned", a.auth(http.HandlerFunc(a.assignedRequests)))
	mux.Handle("GET /api/requests/deleted", a.require("request.view_all", http.HandlerFunc(a.deletedRequests)))
	mux.Handle("GET /api/approvals/mine", a.auth(http.HandlerFunc(a.myApprovals)))
	mux.Handle("GET /api/approvals/history", a.auth(http.HandlerFunc(a.approvalHistory)))
	mux.Handle("GET /api/requests/{id}", a.auth(http.HandlerFunc(a.requestDetail)))
	mux.Handle("GET /api/requests/{id}/timeline", a.auth(http.HandlerFunc(a.requestTimeline)))
	mux.Handle("GET /api/requests/{id}/export", a.auth(http.HandlerFunc(a.exportRequest)))
	mux.Handle("GET /api/requests/{id}/audit-timeline", a.require("request.view_all", http.HandlerFunc(a.requestAuditTimeline)))
	mux.Handle("DELETE /api/requests/{id}", a.auth(http.HandlerFunc(a.deleteRequest)))
	mux.Handle("POST /api/requests/{id}/restore", a.require("request.view_all", http.HandlerFunc(a.restoreRequest)))
	mux.Handle("POST /api/requests/{id}/approve", a.require("request.approve", http.HandlerFunc(a.approveRequest)))
	mux.Handle("POST /api/requests/{id}/status", a.require("request.update_progress", http.HandlerFunc(a.addStatusLog)))
	mux.Handle("POST /api/requests/{id}/pic-stage", a.require("request.update_progress", http.HandlerFunc(a.movePICStage)))
	mux.Handle("POST /api/requests/{id}/comments", a.auth(http.HandlerFunc(a.addComment)))
	mux.Handle("PUT /api/comments/{id}", a.auth(http.HandlerFunc(a.updateComment)))
	mux.Handle("DELETE /api/comments/{id}", a.auth(http.HandlerFunc(a.deleteComment)))
	mux.Handle("POST /api/requests/{id}/result", a.require("request.give_result", http.HandlerFunc(a.giveResult)))
	mux.Handle("POST /api/requests/{id}/attachments", a.auth(http.HandlerFunc(a.uploadAttachment)))
	mux.Handle("DELETE /api/attachments/{id}", a.auth(http.HandlerFunc(a.deleteAttachment)))
	mux.Handle("DELETE /api/results/{id}", a.auth(http.HandlerFunc(a.deleteResult)))
	mux.Handle("GET /uploads/", a.auth(http.HandlerFunc(a.downloadUpload)))

	mux.Handle("GET /api/dashboard", a.auth(http.HandlerFunc(a.dashboard)))
	mux.Handle("GET /api/dashboard/analytics", a.auth(http.HandlerFunc(a.dashboardAnalytics)))
	mux.Handle("GET /api/admin/dashboard", a.require("request.view_all", http.HandlerFunc(a.adminDashboard)))
	mux.Handle("GET /api/audit-logs", a.require("request.view_all", http.HandlerFunc(a.auditLogs)))
	mux.Handle("GET /api/notifications", a.auth(http.HandlerFunc(a.notifications)))
	mux.Handle("GET /api/notifications/unread-count", a.auth(http.HandlerFunc(a.unreadNotificationCount)))
	mux.Handle("POST /api/notifications/{id}/read", a.auth(http.HandlerFunc(a.readNotification)))
	mux.Handle("POST /api/notifications/read-all", a.auth(http.HandlerFunc(a.readAllNotifications)))
	mux.Handle("POST /api/requests/{id}/notifications/read", a.auth(http.HandlerFunc(a.readRequestNotifications)))
	mux.Handle("GET /api/notifications/stream", a.auth(http.HandlerFunc(a.notificationStream)))
	mux.Handle("POST /api/auth/change-password", a.auth(http.HandlerFunc(a.changePassword)))

	return a.cors(mux)
}

func (a *App) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if token == "" || token == header {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := security.VerifyToken(token, a.cfg.JWTSecret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		next.ServeHTTP(w, withUserID(r, claims.UserID))
	})
}

func (a *App) require(permission string, next http.Handler) http.Handler {
	return a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, err := a.hasPermission(currentUserID(r), permission)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusForbidden, "permission denied")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (a *App) hasPermission(userID int64, code string) (bool, error) {
	var exists int
	err := a.db.QueryRow(`
		SELECT 1
		FROM user_roles ur
		JOIN role_permissions rp ON rp.role_id = ur.role_id
		JOIN permissions p ON p.id = rp.permission_id
		WHERE ur.user_id = ? AND p.code = ?
		LIMIT 1`, userID, code).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
