# Database Migration

Urutan import via phpMyAdmin:

1. `000_create_database.sql`
2. `001_schema.sql`
3. `002_seed.sql`

Jika database sudah dibuat manual di phpMyAdmin, pilih database tersebut lalu import `001_schema.sql` dan `002_seed.sql`.

Default akun seed:

- `superadmin@example.com` / `password123`
- `hr@example.com` / `password123`
- `manager@example.com` / `password123`
- `staff@example.com` / `password123`
