-- +goose Up
-- +goose StatementBegin

-- Самый горячий путь: поиск по short_code для редиректа.
-- Partial index только по активным URL — меньше размер, быстрее lookup.
CREATE INDEX idx_urls_short_code ON urls (short_code)
    WHERE status = 'active';

-- Список ссылок пользователя: отсортированный по дате создания,
-- исключаем удалённые через partial index.
CREATE INDEX idx_urls_user_id ON urls (user_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- Outbox poller: быстрый доступ к pending событиям по порядку создания.
CREATE INDEX idx_outbox_pending ON outbox_events (created_at)
    WHERE status = 'pending';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_urls_short_code;
DROP INDEX IF EXISTS idx_urls_user_id;
DROP INDEX IF EXISTS idx_outbox_pending;

-- +goose StatementEnd