package database

import (
	"database/sql"
	"encoding/json"
	"time"

	"rbac-request-engine/internal/security"
)

func Seed(db *sql.DB) error {
	if err := seedMasterData(db); err != nil {
		return err
	}
	if err := seedRolesAndPermissions(db); err != nil {
		return err
	}
	if err := seedRealisticUsers(db); err != nil {
		return err
	}
	return seedRealisticRequests(db)
}

func seedMasterData(db *sql.DB) error {
	if _, err := db.Exec(`INSERT IGNORE INTO departments (id, name) VALUES
		(1,'IT'),
		(2,'Finance'),
		(3,'HR'),
		(4,'GA'),
		(5,'General'),
		(6,'Procurement'),
		(7,'Legal'),
		(8,'Operations'),
		(9,'Warehouse'),
		(10,'Sales')`); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT IGNORE INTO site_locations (id, name, address) VALUES
		(1,'Head Office','Jl. Gatot Subroto Kav. 18, Jakarta Selatan'),
		(2,'Site Jakarta','Kawasan Industri Pulogadung, Jakarta Timur'),
		(3,'Plant Cikarang','Kawasan Industri Jababeka, Cikarang'),
		(4,'Gudang Marunda','Jl. Marunda Makmur, Jakarta Utara'),
		(5,'Site Bandung','Jl. Soekarno Hatta, Bandung'),
		(6,'Site Surabaya','Jl. Rungkut Industri, Surabaya'),
		(7,'Site Medan','Jl. Gatot Subroto, Medan'),
		(8,'Site Makassar','Jl. Perintis Kemerdekaan, Makassar')`)
	return err
}

func seedRolesAndPermissions(db *sql.DB) error {
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
		"hr":          {"user.manage", "request.create", "request.approve", "request.view_all"},
		"manager":     {"request.create", "request.approve"},
		"finance":     {"request.create", "request.approve", "request.view_all"},
		"director":    {"request.create", "request.approve", "request.view_all"},
		"pic":         {"request.update_progress", "request.give_result", "request.view_all"},
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
	return nil
}

func seedRealisticUsers(db *sql.DB) error {
	pass, err := security.HashPassword("password123")
	if err != nil {
		return err
	}
	users := []struct {
		name    string
		email   string
		deptID  int
		siteID  int
		phone   string
		manager string
		roles   []string
	}{
		{"Super Admin", "superadmin@example.com", 1, 1, "0811-1000-0001", "", []string{"super_admin"}},
		{"Ratna Puspita", "ratna.puspita@andalan.local", 8, 1, "0812-1100-0001", "", []string{"director"}},
		{"Budi Santoso", "budi.santoso@andalan.local", 8, 1, "0812-2200-1001", "ratna.puspita@andalan.local", []string{"manager"}},
		{"Andi Pratama", "andi.pratama@andalan.local", 1, 1, "0812-2200-1002", "ratna.puspita@andalan.local", []string{"manager", "pic"}},
		{"Siti Rahma", "siti.rahma@andalan.local", 2, 1, "0812-2200-1003", "ratna.puspita@andalan.local", []string{"finance"}},
		{"Dinda Lestari", "dinda.lestari@andalan.local", 3, 1, "0812-2200-1004", "ratna.puspita@andalan.local", []string{"hr"}},
		{"Raka Wijaya", "raka.wijaya@andalan.local", 1, 1, "0812-3300-2001", "andi.pratama@andalan.local", []string{"pic"}},
		{"Maya Kartika", "maya.kartika@andalan.local", 4, 1, "0812-3300-2002", "budi.santoso@andalan.local", []string{"pic"}},
		{"Fajar Nugroho", "fajar.nugroho@andalan.local", 6, 1, "0812-3300-2003", "budi.santoso@andalan.local", []string{"pic"}},
		{"Rizky Maulana", "rizky.maulana@andalan.local", 7, 1, "0812-3300-2004", "ratna.puspita@andalan.local", []string{"manager"}},
		{"Lina Marlina", "lina.marlina@andalan.local", 10, 2, "0813-4400-3001", "budi.santoso@andalan.local", []string{"staff"}},
		{"Arif Setiawan", "arif.setiawan@andalan.local", 8, 3, "0813-4400-3002", "budi.santoso@andalan.local", []string{"staff"}},
		{"Wulan Permata", "wulan.permata@andalan.local", 9, 4, "0813-4400-3003", "budi.santoso@andalan.local", []string{"staff"}},
		{"Nabila Putri", "nabila.putri@andalan.local", 2, 1, "0813-4400-3004", "siti.rahma@andalan.local", []string{"staff"}},
		{"Tono Saputra", "tono.saputra@andalan.local", 8, 6, "0813-4400-3005", "budi.santoso@andalan.local", []string{"manager"}},
		{"HR Admin", "hr@example.com", 3, 1, "0811-1000-0002", "ratna.puspita@andalan.local", []string{"hr"}},
		{"Manager", "manager@example.com", 5, 2, "0811-1000-0003", "ratna.puspita@andalan.local", []string{"manager"}},
		{"Finance", "finance@example.com", 2, 1, "0811-1000-0004", "ratna.puspita@andalan.local", []string{"finance"}},
		{"PIC Support", "pic@example.com", 1, 1, "0811-1000-0005", "andi.pratama@andalan.local", []string{"pic"}},
		{"Staff", "staff@example.com", 5, 2, "0811-1000-0006", "manager@example.com", []string{"staff"}},
	}
	for _, u := range users {
		if _, err := db.Exec(`
			INSERT INTO users (name, email, password_hash, department_id, site_location_id, phone, status)
			VALUES (?, ?, ?, ?, ?, ?, 'active')
			ON DUPLICATE KEY UPDATE name = VALUES(name), department_id = VALUES(department_id), site_location_id = VALUES(site_location_id), phone = VALUES(phone), status = 'active'`,
			u.name, u.email, pass, u.deptID, u.siteID, u.phone); err != nil {
			return err
		}
		for _, role := range u.roles {
			if _, err := db.Exec(`
				INSERT IGNORE INTO user_roles (user_id, role_id)
				SELECT u.id, r.id FROM users u, roles r WHERE u.email = ? AND r.name = ?`, u.email, role); err != nil {
				return err
			}
		}
	}
	for _, u := range users {
		if u.manager == "" {
			continue
		}
		if _, err := db.Exec(`
			UPDATE users staff
			JOIN users mgr ON mgr.email = ?
			SET staff.manager_id = mgr.id
			WHERE staff.email = ?`, u.manager, u.email); err != nil {
			return err
		}
	}
	return nil
}

func seedRealisticRequests(db *sql.DB) error {
	adminID, err := userIDByEmail(db, "superadmin@example.com")
	if err != nil {
		return err
	}
	managerID, err := userIDByEmail(db, "budi.santoso@andalan.local")
	if err != nil {
		return err
	}
	itManagerID, err := userIDByEmail(db, "andi.pratama@andalan.local")
	if err != nil {
		return err
	}
	financeID, err := userIDByEmail(db, "siti.rahma@andalan.local")
	if err != nil {
		return err
	}
	hrID, err := userIDByEmail(db, "dinda.lestari@andalan.local")
	if err != nil {
		return err
	}
	directorID, err := userIDByEmail(db, "ratna.puspita@andalan.local")
	if err != nil {
		return err
	}
	picITID, err := userIDByEmail(db, "raka.wijaya@andalan.local")
	if err != nil {
		return err
	}
	picGAID, err := userIDByEmail(db, "maya.kartika@andalan.local")
	if err != nil {
		return err
	}
	picProcID, err := userIDByEmail(db, "fajar.nugroho@andalan.local")
	if err != nil {
		return err
	}
	linaID, err := userIDByEmail(db, "lina.marlina@andalan.local")
	if err != nil {
		return err
	}
	arifID, err := userIDByEmail(db, "arif.setiawan@andalan.local")
	if err != nil {
		return err
	}
	wulanID, err := userIDByEmail(db, "wulan.permata@andalan.local")
	if err != nil {
		return err
	}
	nabilaID, err := userIDByEmail(db, "nabila.putri@andalan.local")
	if err != nil {
		return err
	}

	itTypeID, err := ensureRequestType(db, requestTypeSeed{
		Name:    "Permintaan Perangkat IT",
		DeptID:  1,
		SLADays: 3,
		Fields: []map[string]any{
			{"key": "judul", "label": "Judul Kebutuhan", "type": "text", "required": true},
			{"key": "kategori_perangkat", "label": "Kategori Perangkat", "type": "select", "required": true, "options": []string{"Laptop", "Monitor", "Printer", "Keyboard/Mouse", "Aksesoris"}},
			{"key": "lokasi_penggunaan", "label": "Lokasi Penggunaan", "type": "text", "required": true},
			{"key": "alasan", "label": "Alasan Kebutuhan", "type": "textarea", "required": true},
			{"key": "lampiran", "label": "Lampiran Pendukung", "type": "file", "required": false},
		},
		Approvals: []map[string]any{{"level": 1, "type": "manager"}, {"level": 2, "type": "user", "user_id": itManagerID}},
		PICs:      []int64{picITID},
		AdminID:   adminID,
	})
	if err != nil {
		return err
	}
	gaTypeID, err := ensureRequestType(db, requestTypeSeed{
		Name:    "Perbaikan Fasilitas Kantor",
		DeptID:  4,
		SLADays: 2,
		Fields: []map[string]any{
			{"key": "area", "label": "Area/Lokasi", "type": "text", "required": true},
			{"key": "jenis_kerusakan", "label": "Jenis Kerusakan", "type": "select", "required": true, "options": []string{"AC", "Lampu", "Meja/Kursi", "Toilet", "Listrik", "Lainnya"}},
			{"key": "deskripsi", "label": "Deskripsi Masalah", "type": "textarea", "required": true},
			{"key": "foto", "label": "Foto Kondisi", "type": "file", "required": false},
		},
		Approvals: []map[string]any{{"level": 1, "type": "manager"}},
		PICs:      []int64{picGAID},
		AdminID:   adminID,
	})
	if err != nil {
		return err
	}
	finTypeID, err := ensureRequestType(db, requestTypeSeed{
		Name:    "Reimbursement Operasional",
		DeptID:  2,
		SLADays: 5,
		Fields: []map[string]any{
			{"key": "nominal", "label": "Nominal", "type": "number", "required": true},
			{"key": "tanggal_transaksi", "label": "Tanggal Transaksi", "type": "date", "required": true},
			{"key": "keperluan", "label": "Keperluan", "type": "textarea", "required": true},
			{"key": "bukti", "label": "Bukti Pembayaran", "type": "file", "required": true},
		},
		Approvals: []map[string]any{{"level": 1, "type": "manager"}, {"level": 2, "type": "user", "user_id": financeID}},
		PICs:      []int64{financeID},
		AdminID:   adminID,
	})
	if err != nil {
		return err
	}
	hrTypeID, err := ensureRequestType(db, requestTypeSeed{
		Name:    "Onboarding Karyawan Baru",
		DeptID:  3,
		SLADays: 4,
		Fields: []map[string]any{
			{"key": "nama_karyawan", "label": "Nama Karyawan", "type": "text", "required": true},
			{"key": "tanggal_join", "label": "Tanggal Join", "type": "date", "required": true},
			{"key": "departemen_tujuan", "label": "Departemen Tujuan", "type": "text", "required": true},
			{"key": "kebutuhan", "label": "Kebutuhan Onboarding", "type": "checkbox", "required": true, "options": []string{"Email", "Laptop", "ID Card", "Akses HRIS", "Meja Kerja"}},
		},
		Approvals: []map[string]any{{"level": 1, "type": "manager"}, {"level": 2, "type": "user", "user_id": hrID}},
		PICs:      []int64{hrID, picITID},
		AdminID:   adminID,
	})
	if err != nil {
		return err
	}
	procTypeID, err := ensureRequestType(db, requestTypeSeed{
		Name:    "Pengadaan Barang/Jasa",
		DeptID:  6,
		SLADays: 7,
		Fields: []map[string]any{
			{"key": "nama_barang", "label": "Nama Barang/Jasa", "type": "text", "required": true},
			{"key": "estimasi_biaya", "label": "Estimasi Biaya", "type": "number", "required": true},
			{"key": "vendor_rekomendasi", "label": "Vendor Rekomendasi", "type": "text", "required": false},
			{"key": "justifikasi", "label": "Justifikasi", "type": "textarea", "required": true},
		},
		Approvals: []map[string]any{{"level": 1, "type": "manager"}, {"level": 2, "type": "user", "user_id": financeID}, {"level": 3, "type": "user", "user_id": directorID}},
		PICs:      []int64{picProcID},
		AdminID:   adminID,
	})
	if err != nil {
		return err
	}

	requests := []requestSeed{
		{
			Ticket: "ATS-2026-0001", TypeID: itTypeID, RequesterID: linaID, Status: "in_approval", Stage: 0, DaysAgo: 1, DueInDays: 2,
			Fields:    map[string]any{"judul": "Laptop baru untuk Sales Executive", "kategori_perangkat": "Laptop", "lokasi_penggunaan": "Site Jakarta", "alasan": "Laptop lama sering mati saat presentasi customer dan sudah lewat masa pakai."},
			Approvals: []approvalSeed{{managerID, 1, "pending", "Menunggu review Manager Sales", nil}, {itManagerID, 2, "pending", "Menunggu review IT Manager", nil}},
			Comments:  []commentSeed{{linaID, "Mohon diprioritaskan karena ada jadwal presentasi customer minggu ini."}},
			Notifications: []notificationSeed{
				{managerID, "approval_needed", "Approval dibutuhkan untuk permintaan laptop Lina Marlina", false},
			},
		},
		{
			Ticket: "ATS-2026-0002", TypeID: gaTypeID, RequesterID: wulanID, Status: "in_progress", Stage: 2, DaysAgo: 3, DueInDays: 1,
			Fields:    map[string]any{"area": "Gudang Marunda - Dock 2", "jenis_kerusakan": "Lampu", "deskripsi": "Lampu area loading mati 3 titik sehingga aktivitas malam kurang aman."},
			Approvals: []approvalSeed{{managerID, 1, "approve", "Disetujui, perlu segera ditangani karena menyangkut safety.", hoursAgo(58)}},
			Progress:  []progressSeed{{1, "PIC menerima tiket", picGAID, "Tim GA sudah menerima laporan dan menjadwalkan teknisi."}, {2, "Dalam pengerjaan", picGAID, "Teknisi sudah cek panel, menunggu penggantian lampu LED industrial."}},
			Comments:  []commentSeed{{wulanID, "Foto kondisi sudah dikirim lewat lampiran."}, {picGAID, "Estimasi selesai sore ini setelah sparepart datang."}},
			Attachments: []attachmentSeed{
				{"form", "/uploads/Wulan_Permata/foto_lampu_dock2.jpg", "foto_lampu_dock2.jpg", "image/jpeg", 245760, wulanID},
			},
			Notifications: []notificationSeed{{wulanID, "status_update", "Tiket perbaikan lampu sedang dikerjakan GA", false}},
		},
		{
			Ticket: "ATS-2026-0003", TypeID: finTypeID, RequesterID: arifID, Status: "done", Stage: 3, DaysAgo: 8, DueInDays: -1,
			Fields:    map[string]any{"nominal": 875000, "tanggal_transaksi": "2026-07-01", "keperluan": "Pembelian safety shoes untuk operator baru Plant Cikarang."},
			Approvals: []approvalSeed{{managerID, 1, "approve", "Pembelian sesuai kebutuhan operasional.", hoursAgo(150)}, {financeID, 2, "approve", "Dokumen lengkap dan sesuai nominal.", hoursAgo(126)}},
			Progress:  []progressSeed{{1, "Verifikasi Finance", financeID, "Bukti pembayaran dan rekening sudah diverifikasi."}, {2, "Pembayaran diproses", financeID, "Reimbursement masuk batch pembayaran Jumat."}},
			Result:    "Reimbursement Rp875.000 sudah diproses ke rekening karyawan.",
			Comments:  []commentSeed{{arifID, "Terima kasih, dana sudah diterima."}},
			Attachments: []attachmentSeed{
				{"form", "/uploads/Arif_Setiawan/struk_safety_shoes.pdf", "struk_safety_shoes.pdf", "application/pdf", 384120, arifID},
				{"result", "/uploads/Siti_Rahma/bukti_transfer_reimburse.pdf", "bukti_transfer_reimburse.pdf", "application/pdf", 192440, financeID},
			},
			Notifications: []notificationSeed{{arifID, "result_given", "Reimbursement operasional sudah selesai diproses", true}},
		},
		{
			Ticket: "ATS-2026-0004", TypeID: hrTypeID, RequesterID: nabilaID, Status: "in_progress", Stage: 2, DaysAgo: 2, DueInDays: 3,
			Fields:        map[string]any{"nama_karyawan": "Kevin Mahendra", "tanggal_join": "2026-07-15", "departemen_tujuan": "Finance", "kebutuhan": []string{"Email", "Laptop", "ID Card", "Akses HRIS"}},
			Approvals:     []approvalSeed{{financeID, 1, "approve", "Karyawan replacement sudah sesuai manpower plan.", hoursAgo(42)}, {hrID, 2, "approve", "Data kandidat sudah lengkap.", hoursAgo(30)}},
			Progress:      []progressSeed{{1, "HR setup data karyawan", hrID, "Data personal sudah dibuat di HRIS."}, {2, "IT setup akun", picITID, "Email dan akun aplikasi sedang dibuat."}},
			Comments:      []commentSeed{{nabilaID, "Mohon laptop disiapkan sebelum tanggal join."}},
			Notifications: []notificationSeed{{picITID, "request_assigned", "Setup akun onboarding Kevin Mahendra perlu diproses", false}},
		},
		{
			Ticket: "ATS-2026-0005", TypeID: procTypeID, RequesterID: wulanID, Status: "in_approval", Stage: 2, DaysAgo: 4, DueInDays: 3,
			Fields:    map[string]any{"nama_barang": "Hand pallet 2.5 ton", "estimasi_biaya": 9200000, "vendor_rekomendasi": "PT Maju Teknik", "justifikasi": "Unit lama bocor hidrolik dan sering menghambat unloading barang."},
			Approvals: []approvalSeed{{managerID, 1, "approve", "Kebutuhan urgent untuk operasional gudang.", hoursAgo(72)}, {financeID, 2, "approve", "Budget masih tersedia.", hoursAgo(40)}, {directorID, 3, "pending", "Menunggu approval direktur", nil}},
			Comments:  []commentSeed{{wulanID, "Quotation vendor sudah saya lampirkan."}},
			Attachments: []attachmentSeed{
				{"form", "/uploads/Wulan_Permata/quotation_hand_pallet.pdf", "quotation_hand_pallet.pdf", "application/pdf", 512900, wulanID},
			},
			Notifications: []notificationSeed{{directorID, "approval_needed", "Approval direktur dibutuhkan untuk pengadaan hand pallet", false}},
		},
		{
			Ticket: "ATS-2026-0006", TypeID: itTypeID, RequesterID: arifID, Status: "rejected", Stage: 1, DaysAgo: 6, DueInDays: 1,
			Fields:        map[string]any{"judul": "Permintaan monitor tambahan", "kategori_perangkat": "Monitor", "lokasi_penggunaan": "Plant Cikarang", "alasan": "Ingin layar tambahan untuk monitoring produksi."},
			Approvals:     []approvalSeed{{managerID, 1, "reject", "Ditolak sementara, gunakan monitor spare di area admin terlebih dahulu.", hoursAgo(120)}, {itManagerID, 2, "pending", "Tidak diproses karena ditolak manager.", nil}},
			Comments:      []commentSeed{{managerID, "Coba gunakan aset spare dulu, ajukan ulang kalau memang tidak mencukupi."}},
			Notifications: []notificationSeed{{arifID, "status_update", "Permintaan monitor tambahan ditolak oleh manager", true}},
		},
	}
	for _, req := range requests {
		if err := seedRequest(db, req); err != nil {
			return err
		}
	}
	return nil
}

type requestTypeSeed struct {
	Name      string
	DeptID    int64
	SLADays   int
	Fields    []map[string]any
	Approvals []map[string]any
	PICs      []int64
	AdminID   int64
}

func ensureRequestType(db *sql.DB, seed requestTypeSeed) (int64, error) {
	formSchema, _ := json.Marshal(map[string]any{"fields": seed.Fields})
	approvalChain, _ := json.Marshal(seed.Approvals)
	var id int64
	err := db.QueryRow(`SELECT id FROM request_types WHERE name = ? LIMIT 1`, seed.Name).Scan(&id)
	if err == nil {
		_, err = db.Exec(`UPDATE request_types SET department_owner_id = ?, form_schema_json = ?, approval_chain_json = ?, is_active = TRUE, sla_days = ? WHERE id = ?`,
			seed.DeptID, string(formSchema), string(approvalChain), seed.SLADays, id)
		if err != nil {
			return 0, err
		}
	} else if err == sql.ErrNoRows {
		res, err := db.Exec(`
			INSERT INTO request_types (name, department_owner_id, form_schema_json, approval_chain_json, is_active, sla_days, created_by)
			VALUES (?, ?, ?, ?, TRUE, ?, ?)`, seed.Name, seed.DeptID, string(formSchema), string(approvalChain), seed.SLADays, seed.AdminID)
		if err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else {
		return 0, err
	}
	for stage, picID := range seed.PICs {
		if _, err := db.Exec(`
			INSERT INTO request_type_pic (request_type_id, user_id, stage_number, assigned_by)
			VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE stage_number = VALUES(stage_number), assigned_by = VALUES(assigned_by)`,
			id, picID, stage+1, seed.AdminID); err != nil {
			return 0, err
		}
	}
	return id, nil
}

type requestSeed struct {
	Ticket        string
	TypeID        int64
	RequesterID   int64
	Status        string
	Stage         int
	DaysAgo       int
	DueInDays     int
	Fields        map[string]any
	Approvals     []approvalSeed
	Progress      []progressSeed
	Result        string
	Comments      []commentSeed
	Attachments   []attachmentSeed
	Notifications []notificationSeed
}

type approvalSeed struct {
	ApproverID int64
	Level      int
	Action     string
	Note       string
	ActedAt    *time.Time
}

type progressSeed struct {
	Stage  int
	Status string
	UserID int64
	Note   string
}

type commentSeed struct {
	UserID  int64
	Message string
}

type attachmentSeed struct {
	SourceType string
	FileURL    string
	FileName   string
	MimeType   string
	FileSize   int64
	UserID     int64
}

type notificationSeed struct {
	UserID  int64
	Type    string
	Message string
	IsRead  bool
}

func seedRequest(db *sql.DB, seed requestSeed) error {
	requestID, err := ensureRequest(db, seed)
	if err != nil {
		return err
	}
	for key, value := range seed.Fields {
		raw, _ := json.Marshal(value)
		if _, err := db.Exec(`
			INSERT INTO request_fields (request_id, field_key, field_value)
			VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE field_value = VALUES(field_value)`, requestID, key, string(raw)); err != nil {
			return err
		}
	}
	for _, approval := range seed.Approvals {
		if err := seedApproval(db, requestID, approval); err != nil {
			return err
		}
	}
	for _, progress := range seed.Progress {
		if err := seedProgress(db, requestID, progress); err != nil {
			return err
		}
	}
	for _, comment := range seed.Comments {
		if err := seedComment(db, requestID, comment); err != nil {
			return err
		}
	}
	for _, attachment := range seed.Attachments {
		if err := seedAttachment(db, requestID, attachment); err != nil {
			return err
		}
	}
	if seed.Result != "" {
		if err := seedResult(db, requestID, seed); err != nil {
			return err
		}
	}
	for _, notification := range seed.Notifications {
		if err := seedNotification(db, requestID, notification); err != nil {
			return err
		}
	}
	return nil
}

func ensureRequest(db *sql.DB, seed requestSeed) (int64, error) {
	createdAt := time.Now().AddDate(0, 0, -seed.DaysAgo)
	dueAt := time.Now().AddDate(0, 0, seed.DueInDays)
	var id int64
	err := db.QueryRow(`SELECT id FROM requests WHERE ticket_number = ? LIMIT 1`, seed.Ticket).Scan(&id)
	if err == nil {
		_, err = db.Exec(`
			UPDATE requests SET request_type_id = ?, user_id = ?, overall_status = ?, current_stage = ?, due_at = ?, deleted_at = NULL WHERE id = ?`,
			seed.TypeID, seed.RequesterID, seed.Status, seed.Stage, dueAt, id)
		return id, err
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := db.Exec(`
		INSERT INTO requests (ticket_number, request_type_id, user_id, overall_status, current_stage, due_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, seed.Ticket, seed.TypeID, seed.RequesterID, seed.Status, seed.Stage, dueAt, createdAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func seedApproval(db *sql.DB, requestID int64, seed approvalSeed) error {
	dueAt := time.Now().AddDate(0, 0, seed.Level)
	_, err := db.Exec(`
		INSERT INTO approvals (request_id, approver_id, level, action, note, acted_at, due_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE approver_id = VALUES(approver_id), action = VALUES(action), note = VALUES(note), acted_at = VALUES(acted_at), due_at = VALUES(due_at)`,
		requestID, seed.ApproverID, seed.Level, seed.Action, seed.Note, nullableTime(seed.ActedAt), dueAt)
	return err
}

func seedProgress(db *sql.DB, requestID int64, seed progressSeed) error {
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_status_log WHERE request_id = ? AND stage_number = ? AND note = ?`, requestID, seed.Stage, seed.Note).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	_, err := db.Exec(`INSERT INTO request_status_log (request_id, stage_number, status_text, updated_by_user_id, note) VALUES (?, ?, ?, ?, ?)`,
		requestID, seed.Stage, seed.Status, seed.UserID, seed.Note)
	return err
}

func seedComment(db *sql.DB, requestID int64, seed commentSeed) error {
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_comments WHERE request_id = ? AND user_id = ? AND message = ?`, requestID, seed.UserID, seed.Message).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	_, err := db.Exec(`INSERT INTO request_comments (request_id, user_id, message) VALUES (?, ?, ?)`, requestID, seed.UserID, seed.Message)
	return err
}

func seedAttachment(db *sql.DB, requestID int64, seed attachmentSeed) error {
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM attachments WHERE request_id = ? AND file_url = ?`, requestID, seed.FileURL).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	_, err := db.Exec(`
		INSERT INTO attachments (request_id, source_type, file_url, file_name, mime_type, file_size, uploaded_by_user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		requestID, seed.SourceType, seed.FileURL, seed.FileName, seed.MimeType, seed.FileSize, seed.UserID)
	return err
}

func seedResult(db *sql.DB, requestID int64, seed requestSeed) error {
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_results WHERE request_id = ?`, requestID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	userID := seed.RequesterID
	if len(seed.Progress) > 0 {
		userID = seed.Progress[len(seed.Progress)-1].UserID
	}
	_, err := db.Exec(`INSERT INTO request_results (request_id, result_text, given_by_user_id) VALUES (?, ?, ?)`, requestID, seed.Result, userID)
	return err
}

func seedNotification(db *sql.DB, requestID int64, seed notificationSeed) error {
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND request_id = ? AND type = ? AND message = ?`, seed.UserID, requestID, seed.Type, seed.Message).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	_, err := db.Exec(`INSERT INTO notifications (user_id, request_id, type, message, is_read) VALUES (?, ?, ?, ?, ?)`,
		seed.UserID, requestID, seed.Type, seed.Message, seed.IsRead)
	return err
}

func userIDByEmail(db *sql.DB, email string) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id)
	return id, err
}

func hoursAgo(hours int) *time.Time {
	t := time.Now().Add(-time.Duration(hours) * time.Hour)
	return &t
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}
