-- +goose Up
ALTER TABLE players ADD COLUMN premium BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE players DROP COLUMN premium;
