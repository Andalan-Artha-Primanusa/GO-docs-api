package app

import "net/http"

func (a *App) workflowMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"statuses": []map[string]any{
			{"code": "pending", "label": "Draft/Pending", "tone": "muted", "icon": "circle-dashed", "actions": []string{"comment", "delete"}},
			{"code": "in_approval", "label": "Menunggu Approval", "tone": "amber", "icon": "clock", "actions": []string{"approve", "reject", "comment", "delete"}},
			{"code": "in_progress", "label": "Sedang Diproses", "tone": "blue", "icon": "loader", "actions": []string{"update_progress", "move_pic_stage", "give_result", "comment"}},
			{"code": "done", "label": "Selesai", "tone": "green", "icon": "check-circle", "actions": []string{"comment", "export"}},
			{"code": "rejected", "label": "Ditolak", "tone": "red", "icon": "x-circle", "actions": []string{"comment", "export"}},
			{"code": "deleted", "label": "Arsip", "tone": "muted", "icon": "archive", "actions": []string{"restore", "export"}},
		},
		"events": []map[string]any{
			{"code": "request_created", "label": "Pengajuan dibuat", "icon": "file-plus"},
			{"code": "approval_requested", "label": "Approval diminta", "icon": "clock"},
			{"code": "approved", "label": "Disetujui", "icon": "check-circle"},
			{"code": "rejected", "label": "Ditolak", "icon": "x-circle"},
			{"code": "progress_update", "label": "Progress diperbarui", "icon": "activity"},
			{"code": "comment", "label": "Komentar", "icon": "message-square"},
			{"code": "result", "label": "Hasil", "icon": "badge-check"},
			{"code": "attachment", "label": "Lampiran", "icon": "paperclip"},
		},
	})
}
