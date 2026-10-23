-- +goose Up
-- 举报与皮肤审核：reports 表 + skinlib_items.status（listed/under_review/unlisted）。

ALTER TABLE skinlib_items ADD COLUMN status TEXT NOT NULL DEFAULT 'listed';
CREATE INDEX idx_skinlib_items_status ON skinlib_items (status);

CREATE TABLE reports (
    id               TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    reporter_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type      TEXT NOT NULL,          -- skin | comment
    target_id        TEXT NOT NULL,
    reason           TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending', -- pending | dismissed | acted
    reviewed_by      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at      TIMESTAMPTZ,
    UNIQUE (reporter_user_id, target_type, target_id)
);
CREATE INDEX idx_reports_status ON reports (status);
