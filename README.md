# RBAC Generic Request Engine

Backend Go + MySQL untuk sistem pengajuan generik dengan RBAC, form dinamis, approval berjenjang, PIC per jenis pengajuan, timeline progres, komentar, hasil, notifikasi, dan audit log.

## Menjalankan

1. Buat database MySQL:

```sql
CREATE DATABASE rbac_request_engine CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

2. Salin konfigurasi:

```powershell
cd backend
Copy-Item .env.example .env
```

3. Sesuaikan `MYSQL_DSN` dan `APP_JWT_SECRET`.

Untuk menyimpan hasil upload ke NAS, connect/mount NAS terlebih dahulu lewat SMB/WebDAV/FTP client di Windows, lalu arahkan `UPLOAD_DIR` ke folder tersebut:

```env
UPLOAD_DIR=Z:\AndalanTicketUploads
```

Atau langsung memakai UNC path SMB jika proses Go punya akses ke share:

```env
UPLOAD_DIR=\\192.168.1.10\TicketShare\Uploads
```

URL file di API tetap memakai format `/uploads/{request_id}/{nama_file}`, tetapi file fisiknya tersimpan di folder NAS.

Jika server tidak mengizinkan mount SMB/CIFS, gunakan mode SFTP:

```env
UPLOAD_STORAGE=sftp
SFTP_HOST=160.19.165.217
SFTP_PORT=22
SFTP_USER=artha
SFTP_PASSWORD=OT6gfpTp
SFTP_DIR=/E-Ticketing/uploads
```

Dengan mode ini backend langsung mengirim file ke NAS via SFTP tanpa `mount`.

Jika NAS hanya menyediakan FTP/FTPS dan bukan SFTP, aktifkan FTP di NAS lalu gunakan mode FTP:

```env
UPLOAD_STORAGE=ftp
FTP_HOST=160.19.165.217
FTP_PORT=21
FTP_USER=artha
FTP_PASSWORD=OT6gfpTp
FTP_DIR=/E-Ticketing/uploads
FTP_TLS=false
```

Pastikan folder `uploads` sudah dibuat di dalam shared folder `E-Ticketing`. File akan disimpan per nama user, misalnya `E-Ticketing/uploads/Super_Admin/nama_file.png`.

4. Install dependensi dan jalankan migrasi:

```powershell
cd backend
go mod tidy
go run ./cmd/api -migrate -seed
go run ./cmd/api
```

Jika folder belum menjadi git repository dan `go build` menampilkan error VCS stamping, gunakan:

```powershell
cd backend
go build -buildvcs=false ./cmd/api
```

## Menjalankan UI React

Backend API berjalan di `http://localhost:3000`.

Frontend React berjalan di `http://localhost:5173`.

```powershell
cd frontend
npm install
npm run dev
```

Jika ingin build production:

```powershell
cd frontend
npm run build
```

Default seed membuat akun:

- `superadmin@example.com` / `password123`
- `hr@example.com` / `password123`
- `manager@example.com` / `password123`
- `staff@example.com` / `password123`
- `ratna.puspita@andalan.local` / `password123` (Director)
- `budi.santoso@andalan.local` / `password123` (Manager Operations)
- `andi.pratama@andalan.local` / `password123` (IT Manager/PIC)
- `siti.rahma@andalan.local` / `password123` (Finance)
- `dinda.lestari@andalan.local` / `password123` (HR)
- `raka.wijaya@andalan.local` / `password123` (PIC IT)
- `maya.kartika@andalan.local` / `password123` (PIC GA)
- `fajar.nugroho@andalan.local` / `password123` (PIC Procurement)
- `lina.marlina@andalan.local` / `password123` (Staff Sales)
- `arif.setiawan@andalan.local` / `password123` (Staff Operations)
- `wulan.permata@andalan.local` / `password123` (Staff Warehouse)
- `nabila.putri@andalan.local` / `password123` (Staff Finance)

Semua akun login lewat endpoint yang sama: `POST /api/auth/login`. Yang membedakan akses adalah role, departemen, dan lokasi site milik user.

| Email | Password | Role | Departemen | Lokasi Site |
|---|---|---|---|---|
| `superadmin@example.com` | `password123` | `super_admin` | IT | Head Office |
| `hr@example.com` | `password123` | `hr` | HR | Head Office |
| `manager@example.com` | `password123` | `manager` | General | Site Jakarta |
| `staff@example.com` | `password123` | `staff` | General | Site Jakarta |

## Migration Database

Migration SQL siap import ada di folder `migrations/`:

- `000_create_database.sql`
- `001_schema.sql`
- `002_seed.sql`
- `003_add_site_locations.sql` khusus untuk database lama yang sudah dibuat sebelum ada kolom lokasi site

Untuk phpMyAdmin, import berurutan dari `000`, `001`, lalu `002`. Kalau database sudah dibuat dan dipilih di phpMyAdmin, cukup import `001_schema.sql` lalu `002_seed.sql`.

## Endpoint Utama

- `POST /api/auth/register`
- `POST /api/auth/login`
- `GET /api/me`
- `GET /api/departments`
- `GET /api/site-locations`
- `GET /api/request-types`
- `POST /api/request-types` (`request_type.manage`)
- `PUT /api/request-types/{id}` (`request_type.manage`)
- `POST /api/request-types/{id}/pics` (`request_type.assign_pic`)
- `POST /api/requests` (`request.create`)
- `GET /api/requests/mine`
- `GET /api/requests/assigned`
- `GET /api/requests/{id}`
- `POST /api/requests/{id}/approve` (`request.approve`)
- `POST /api/requests/{id}/status` (`request.update_progress`, PIC)
- `POST /api/requests/{id}/comments`
- `POST /api/requests/{id}/result` (`request.give_result`, PIC)
- `GET /api/dashboard`
- `GET /api/notifications`
- `POST /api/notifications/{id}/read`

## Format Response API

Semua endpoint memakai format response konsisten:

```json
{
  "statusCode": 200,
  "success": true,
  "message": "OK",
  "data": {}
}
```

Contoh response error:

```json
{
  "statusCode": 400,
  "success": false,
  "message": "Bad Request",
  "error": "name, email, and password are required"
}
```

## Postman

File siap import ada di folder `backend/postman/`:

- `RBAC Generic Request Engine.postman_collection.json`
- `RBAC Generic Request Engine.postman_environment.json`

Urutan cepat:

1. Import collection dan environment ke Postman.
2. Pilih environment `RBAC Generic Request Engine - Local`.
3. Jalankan `Login (Semua Role)`; token otomatis tersimpan ke variable `token`.
4. Jalankan `Create Request Type`, `Assign PICs`, lalu login sebagai `staff@example.com` untuk membuat pengajuan.

## Format Form Schema

`form_schema_json` disimpan sebagai JSON bebas. Contoh:

```json
{
  "fields": [
    {"key": "title", "label": "Judul", "type": "text", "required": true},
    {"key": "amount", "label": "Nominal", "type": "number", "required": false}
  ]
}
```

## Format Approval Chain

```json
[
  {"level": 1, "type": "manager"},
  {"level": 2, "type": "role", "role": "finance"}
]
```

Tipe approver yang didukung:

- `manager`: memakai `manager_id` dari pembuat pengajuan.
- `user`: memakai `user_id` eksplisit.
- `role`: memilih user pertama yang punya role tersebut.
