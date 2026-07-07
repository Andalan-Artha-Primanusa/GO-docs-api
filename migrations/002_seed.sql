INSERT IGNORE INTO departments (id, name) VALUES
  (1, 'IT'),
  (2, 'Finance'),
  (3, 'HR'),
  (4, 'GA'),
  (5, 'General');

INSERT IGNORE INTO site_locations (id, name, address) VALUES
  (1, 'Head Office', 'Kantor pusat'),
  (2, 'Site Jakarta', 'Jakarta'),
  (3, 'Site Bandung', 'Bandung'),
  (4, 'Site Surabaya', 'Surabaya');

INSERT IGNORE INTO permissions (code, description) VALUES
  ('user.manage', 'user.manage'),
  ('role.manage', 'role.manage'),
  ('request_type.manage', 'request_type.manage'),
  ('request_type.assign_pic', 'request_type.assign_pic'),
  ('request.create', 'request.create'),
  ('request.approve', 'request.approve'),
  ('request.update_progress', 'request.update_progress'),
  ('request.give_result', 'request.give_result'),
  ('request.view_all', 'request.view_all');

INSERT IGNORE INTO roles (name, description) VALUES
  ('super_admin', 'super_admin'),
  ('hr', 'hr'),
  ('manager', 'manager'),
  ('finance', 'finance'),
  ('staff', 'staff');

INSERT IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN (
  'user.manage',
  'role.manage',
  'request_type.manage',
  'request_type.assign_pic',
  'request.create',
  'request.approve',
  'request.update_progress',
  'request.give_result',
  'request.view_all'
)
WHERE r.name = 'super_admin';

INSERT IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('user.manage', 'request.create', 'request.approve')
WHERE r.name = 'hr';

INSERT IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('request.create', 'request.approve')
WHERE r.name = 'manager';

INSERT IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('request.create', 'request.approve')
WHERE r.name = 'finance';

INSERT IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('request.create')
WHERE r.name = 'staff';

INSERT IGNORE INTO users (id, name, email, password_hash, department_id, site_location_id, status) VALUES
  (1, 'Super Admin', 'superadmin@example.com', 'sha256$120000$MDEyMzQ1Njc4OWFiY2RlZg$TPmuscEAbpfcx8VOYsffVx46x2v6wXSWyjuP1gC/194', 1, 1, 'active'),
  (2, 'HR Admin', 'hr@example.com', 'sha256$120000$MDEyMzQ1Njc4OWFiY2RlZg$TPmuscEAbpfcx8VOYsffVx46x2v6wXSWyjuP1gC/194', 3, 1, 'active'),
  (3, 'Manager', 'manager@example.com', 'sha256$120000$MDEyMzQ1Njc4OWFiY2RlZg$TPmuscEAbpfcx8VOYsffVx46x2v6wXSWyjuP1gC/194', 5, 2, 'active'),
  (4, 'Staff', 'staff@example.com', 'sha256$120000$MDEyMzQ1Njc4OWFiY2RlZg$TPmuscEAbpfcx8VOYsffVx46x2v6wXSWyjuP1gC/194', 5, 2, 'active');

UPDATE users staff
JOIN users mgr ON mgr.email = 'manager@example.com'
SET staff.manager_id = mgr.id
WHERE staff.email = 'staff@example.com';

INSERT IGNORE INTO user_roles (user_id, role_id)
SELECT u.id, r.id FROM users u JOIN roles r ON r.name = 'super_admin'
WHERE u.email = 'superadmin@example.com';

INSERT IGNORE INTO user_roles (user_id, role_id)
SELECT u.id, r.id FROM users u JOIN roles r ON r.name = 'hr'
WHERE u.email = 'hr@example.com';

INSERT IGNORE INTO user_roles (user_id, role_id)
SELECT u.id, r.id FROM users u JOIN roles r ON r.name = 'manager'
WHERE u.email = 'manager@example.com';

INSERT IGNORE INTO user_roles (user_id, role_id)
SELECT u.id, r.id FROM users u JOIN roles r ON r.name = 'staff'
WHERE u.email = 'staff@example.com';
