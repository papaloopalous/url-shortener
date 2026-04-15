-- +goose Up
-- +goose StatementBegin

CREATE INDEX idx_click_events_short_code ON click_events (short_code, clicked_at DESC);

CREATE INDEX idx_click_events_country ON click_events (short_code, country)
    WHERE country IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_click_events_short_code;
DROP INDEX IF EXISTS idx_click_events_country;

-- +goose StatementEnd