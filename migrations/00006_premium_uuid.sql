-- +goose Up
ALTER TABLE players ADD COLUMN premium_uuid TEXT NOT NULL DEFAULT '';
ALTER TABLE players DROP COLUMN premium;

-- +goose Down
ALTER TABLE players ADD COLUMN premium BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE players DROP COLUMN premium_uuid;
