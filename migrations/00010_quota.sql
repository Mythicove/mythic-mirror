-- +goose Up
-- 配额支撑：用户存储 tier + 纹理文件登记（按上传者归属，sha256 键去重）。

ALTER TABLE users ADD COLUMN storage_tier TEXT NOT NULL DEFAULT 'free';

CREATE TABLE files (
    key           TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL,
    size_bytes    BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_files_owner ON files (owner_user_id);
