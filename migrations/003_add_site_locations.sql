CREATE TABLE IF NOT EXISTS site_locations (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(120) NOT NULL UNIQUE,
  address VARCHAR(255) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO site_locations (id, name, address) VALUES
  (1, 'Head Office', 'Kantor pusat'),
  (2, 'Site Jakarta', 'Jakarta'),
  (3, 'Site Bandung', 'Bandung'),
  (4, 'Site Surabaya', 'Surabaya');

ALTER TABLE users
  ADD COLUMN site_location_id BIGINT NULL AFTER department_id;

UPDATE users SET site_location_id = 1 WHERE email IN ('superadmin@example.com', 'hr@example.com') AND site_location_id IS NULL;
UPDATE users SET site_location_id = 2 WHERE email IN ('manager@example.com', 'staff@example.com') AND site_location_id IS NULL;
