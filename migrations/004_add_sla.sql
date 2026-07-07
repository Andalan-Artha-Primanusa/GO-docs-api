ALTER TABLE request_types
  ADD COLUMN sla_days INT NULL AFTER is_active;

ALTER TABLE requests
  ADD COLUMN due_at TIMESTAMP NULL AFTER current_stage;
