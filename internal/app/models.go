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
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`

	ApprovalTotal         int        `json:"approval_total"`
	ApprovedTotal         int        `json:"approved_total"`
	LastApproverName      *string    `json:"last_approver_name,omitempty"`
	LastApprovedAt        *time.Time `json:"last_approved_at,omitempty"`
	NextApproverName      *string    `json:"next_approver_name,omitempty"`
	NextApprovalLevel     *int       `json:"next_approval_level,omitempty"`
	NextApprovalCreatedAt *time.Time `json:"next_approval_created_at,omitempty"`
	NextApprovalDueAt     *time.Time `json:"next_approval_due_at,omitempty"`
	ApprovalLabel         string     `json:"approval_label"`
	LastActionLabel       string     `json:"last_action_label"`
	NextActionLabel       string     `json:"next_action_label"`
}

func (r *RequestSummary) fillLabels() {
	if r.ApprovalTotal > 0 {
		r.ApprovalLabel = intLabel(r.ApprovedTotal) + "/" + intLabel(r.ApprovalTotal) + " approval selesai"
	} else {
		r.ApprovalLabel = "Tanpa approval"
	}
	if r.LastApproverName != nil {
		r.LastActionLabel = "Terakhir disetujui oleh " + *r.LastApproverName
	} else if r.OverallStatus == "rejected" {
		r.LastActionLabel = "Pengajuan ditolak"
	} else {
		r.LastActionLabel = "Belum ada approval"
	}
	if r.DeletedAt != nil {
		r.NextActionLabel = "Diarsipkan"
		return
	}
	if r.OverallStatus == "done" {
		r.NextActionLabel = "Selesai"
		return
	}
	if r.OverallStatus == "rejected" {
		r.NextActionLabel = "Ditolak"
		return
	}
	if r.NextApproverName != nil {
		r.NextActionLabel = "Menunggu " + *r.NextApproverName
		return
	}
	if r.OverallStatus == "in_progress" {
		r.NextActionLabel = "Menunggu proses PIC"
		return
	}
	r.NextActionLabel = "Menunggu proses berikutnya"
}

func intLabel(v int) string {
	if v == 0 {
		return "0"
	}
	var out [20]byte
	i := len(out)
	for v > 0 {
		i--
		out[i] = byte('0' + v%10)
		v /= 10
	}
	return string(out[i:])
}
