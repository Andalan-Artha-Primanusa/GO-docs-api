package app

import "time"

type User struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	DepartmentID   *int64    `json:"department_id,omitempty"`
	Department     *string   `json:"department,omitempty"`
	SiteLocationID *int64    `json:"site_location_id,omitempty"`
	SiteLocation   *string   `json:"site_location,omitempty"`
	ManagerID      *int64    `json:"manager_id,omitempty"`
	Phone          *string   `json:"phone,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	Roles          []string  `json:"roles,omitempty"`
	Permissions    []string  `json:"permissions,omitempty"`
}

type RequestSummary struct {
	ID              int64      `json:"id"`
	TicketNumber    string     `json:"ticket_number"`
	RequestTypeID   int64      `json:"request_type_id"`
	RequestTypeName string     `json:"request_type_name"`
	UserID          int64      `json:"user_id"`
	OverallStatus   string     `json:"overall_status"`
	CurrentStage    int        `json:"current_stage"`
	DueAt           *time.Time `json:"due_at,omitempty"`
	IsOverdue       bool       `json:"is_overdue"`
	CreatedAt       time.Time  `json:"created_at"`
}
