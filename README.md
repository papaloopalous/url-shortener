# shortener-service

Микросервис сокращения URL.

Отвечает за создание коротких ссылок, редирект по коду, аналитику кликов и
публикацию событий в Kafka через паттерн **Transactional Outbox**.

---

## Архитектура

Сервис построен по принципам **Clean Architecture** и разделён на слои:

```
controller/http   -  HTTP запросы, JWT middleware, DTO
usecase           -  бизнес-логика (создание, редирект, удаление)
domain            -  сущности (URL, OutboxEvent), интерфейсы-порты
adapters          -  PostgreSQL, Redis, Kafka, gRPC auth-client
outbox            -  горутина Transactional Outbox poller
```

**Взаимодействие с другими сервисами:**

```
HTTP client  -  shortener-service (8082)
                    | gRPC
                auth-service (9091) - валидация JWT
                    | PostgreSQL
                shortener_db - хранилище URL + outbox_events
                    | Redis
                кэш short_code - URL (24ч TTL)
                    | Kafka (через outbox poller)
        analytics-service (подписчик click-events)
```

---

## API

### `POST /urls` - создать короткую ссылку

**Требует:** `Authorization: Bearer <token>`

```bash
curl -X POST http://localhost:8082/urls \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"long_url": "https://example.com/very/long/path", "ttl_days": 30}'
```

**Ответ 201:**
```json
{
  "short_code": "aB3xK7z",
  "short_url":  "http://localhost:8082/aB3xK7z",
  "long_url":   "https://example.com/very/long/path",
  "expires_at": "2025-11-10T12:00:00Z"
}
```

---

### `GET /{code}` - редирект (публичный)

```bash
curl -L http://localhost:8082/aB3xK7z
```

**Ответы:**
- `302 Found` + `Location: <long_url>` - успех
- `404 Not Found` - код не существует
- `410 Gone` - ссылка истекла или удалена

---

### `GET /urls` - список своих ссылок

**Требует:** `Authorization: Bearer <token>`

```bash
curl http://localhost:8082/urls \
  -H "Authorization: Bearer $TOKEN"
```

**Ответ 200:**
```json
[
  {
    "short_code": "aB3xK7z",
    "short_url":  "http://localhost:8082/aB3xK7z",
    "long_url":   "https://example.com/very/long/path",
    "status":     "active",
    "expires_at": "2025-11-10T12:00:00Z",
    "created_at": "2025-08-01T10:00:00Z"
  }
]
```

---

### `DELETE /urls` - batch soft delete

**Требует:** `Authorization: Bearer <token>`

```bash
curl -X DELETE http://localhost:8082/urls \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"codes": ["aB3xK7z", "cD4yL8w"]}'
```

**Ответ 200:**
```json
{ "deleted": 2 }
```

> Удаляются только ссылки, принадлежащие текущему пользователю.
> Чужие коды молча игнорируются (ownership check в SQL).

---

### `GET /healthz` - liveness probe

```bash
curl http://localhost:8082/healthz
# - 200 OK
```

### `GET /metrics` - Prometheus scrape

```bash
curl http://localhost:8082/metrics
```

---

## Конфигурация

| Переменная | Описание | Пример |
|---|---|---|
| `APP_ENV` | Режим (development/production) | `development` |
| `HTTP_ADDR` | Адрес HTTP сервера | `:8082` |
| `POSTGRES_DSN` | DSN PostgreSQL | `postgres://user:pass@host:5432/db` |
| `PG_MAX_CONNS` | Макс. соединений в пуле | `20` |
| `PG_MIN_CONNS` | Мин. соединений в пуле | `2` |
| `REDIS_ADDR` | Адрес Redis | `localhost:6379` |
| `REDIS_PASSWORD` | Пароль Redis | `` |
| `REDIS_DB` | Номер БД Redis | `1` |
| `KAFKA_BROKERS` | Список брокеров через запятую | `localhost:9092` |
| `KAFKA_URL_EVENTS_TOPIC` | Топик для url.created/deleted | `url-events` |
| `KAFKA_CLICK_EVENTS_TOPIC` | Топик для url.clicked | `click-events` |
| `AUTH_GRPC_ADDR` | gRPC адрес auth-service | `localhost:9091` |
| `BASE_URL` | Базовый URL для short_url | `http://localhost:8082` |
| `DEFAULT_URL_TTL_DAYS` | TTL по умолчанию (дни) | `90` |
| `OUTBOX_BATCH_SIZE` | Размер батча outbox poller | `100` |
| `OUTBOX_POLL_INTERVAL` | Интервал опроса outbox | `1s` |
| `OTLP_ENDPOINT` | Адрес Jaeger OTLP/HTTP | `localhost:4318` |

---

## Запуск локально

### Через Docker Compose

```bash
cp .env.example .env
make docker-up
```

**Доступные UI после запуска:**
- Grafana: http://localhost:3000 (admin/admin)
- Jaeger: http://localhost:16686
- Prometheus: http://localhost:9090
- Kibana: http://localhost:5601

### Локально без Docker

```bash
cp .env.example .env
# Отредактировать .env, указав локальные адреса

make build
./bin/shortener-service
```

---

## Тесты

### Unit тесты (gomock, без внешних зависимостей)

Покрывают:
- **`usecase/url_test.go`** - Create (success, collision retry, cache fail best-effort), Redirect (cache hit, cache miss + populate, expired, deleted, not found), BatchDelete (success, not owner)
- **`outbox/poller_test.go`** - успешный цикл, частичный failure (оставляем pending), graceful shutdown, пустой батч
- **`controller/http/url_test.go`** - все эндпоинты: 201, 401, 302, 404, 410, 200

### Integration тесты (testcontainers, поднимают реальные контейнеры)

Покрывают:
- **`adapters/db/postgres/repo_test.go`** - атомарность CreateWithURL, SoftDeleteBatch с ownership check, PendingBatch c SKIP LOCKED, MarkPublished
- **`adapters/cache/redis/url_cache_test.go`** - Get/Set/Delete/DeleteBatch, TTL expiry
- **`adapters/kafka/producer_test.go`** - Publish + consume для проверки доставки

### Coverage

```bash
make test-cover
```

---

## CI/CD

### GitHub Actions

Pipeline из 4 стадий на ветке `shortener-service`:

```
lint - test-unit - test-integration - build - docker
```

- **lint** - golangci-lint v1.59
- **test-unit** - unit тесты с `-race`
- **test-integration** - testcontainers (Docker-in-Docker)
- **build** - компиляция бинарника, артефакт
- **docker** - сборка Docker образа

---

## Observability

### Трейсинг (Jaeger)

Сервис использует OpenTelemetry с OTLP/HTTP экспортёром в Jaeger.

Трейсы создаются в:
- `URLUsecase.Create` - атрибуты: `url.short_code`, `url.user_id`
- `URLUsecase.Redirect` - атрибуты: `url.short_code`, `url.cache_hit`
- `URLUsecase.BatchDelete` - атрибуты: `url.user_id`, `url.codes_count`
- `Poller.poll` - атрибуты: `outbox.batch_size`, `outbox.published`
- HTTP middleware - инжектирует span в каждый запрос

Jaeger UI: **http://localhost:16686**

### Метрики (Prometheus + Grafana)

| Метрика | Тип | Labels |
|---|---|---|
| `shortener_http_requests_total` | Counter | method, path, status |
| `shortener_http_request_duration_seconds` | Histogram | method, path |
| `shortener_events_total` | Counter | event |
| `shortener_outbox_pending_total` | Gauge | - |

Значения `event`: `url_created`, `url_redirected`, `url_deleted`, `cache_hit`, `cache_miss`, `outbox_published`, `outbox_failed`

**Grafana dashboard** (`shortener-service.json`) включает 4 секции:
1. HTTP RED (rate, errors, latency p50/p95/p99)
2. Redirect rate + cache hit ratio
3. Outbox: published rate, failed rate, pending gauge
4. Business events per second

Grafana UI: **http://localhost:3000**

### Логи (ELK Stack)

Логи в формате JSON (slog) - Filebeat - Logstash - Elasticsearch - Kibana.

В `development` режиме логи выводятся в текстовом формате для читаемости.

Kibana UI: **http://localhost:5601**

---

## База данных

### Схема

```sql
-- Таблица коротких ссылок
urls (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL,          -- владелец (из JWT)
    short_code  TEXT UNIQUE NOT NULL,   -- 7 символов Base62
    long_url    TEXT NOT NULL,
    status      TEXT DEFAULT 'active',  -- active | soft_deleted
    expires_at  TIMESTAMPTZ,            -- NULL = не истекает
    created_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ,
    deleted_at  TIMESTAMPTZ             -- NULL = не удалён (soft delete)
)

-- Таблица Transactional Outbox
outbox_events (
    id           UUID PRIMARY KEY,
    event_type   TEXT,                  -- url.created | url.clicked | url.deleted
    payload      JSONB,
    status       TEXT DEFAULT 'pending',-- pending | published
    created_at   TIMESTAMPTZ,
    published_at TIMESTAMPTZ
)
```

### Индексы

```sql
-- Самый горячий путь: GET /{code} - редирект
-- Partial index только по активным URL: меньше размер индекса, быстрее lookup
CREATE INDEX idx_urls_short_code ON urls (short_code) WHERE status = 'active';

-- GET /urls: список ссылок пользователя по дате
-- Partial index исключает удалённые - не показываем их в списке
CREATE INDEX idx_urls_user_id ON urls (user_id, created_at DESC) WHERE deleted_at IS NULL;

-- Outbox poller: быстрый доступ к очереди событий
-- Partial index только по pending - published события не нужны при поллинге
CREATE INDEX idx_outbox_pending ON outbox_events (created_at) WHERE status = 'pending';
```

### Soft Delete

При удалении URL через `DELETE /urls` сервис **не удаляет строки физически**.
Вместо этого устанавливается `deleted_at` и `status = 'soft_deleted'`.

Это позволяет:
- Сохранять аналитику кликов до мягко удалённых ссылок
- Выполнять bulk hard delete по расписанию (отдельный cleanup-service)
- Легко восстановить ссылку при необходимости

Cleanup-service (не входит в данный репозиторий) периодически делает
`DELETE FROM urls WHERE deleted_at < now() - interval '30 days'`.

---

## Паттерн Transactional Outbox

### Проблема

Наивная реализация публикует событие в Kafka **внутри** той же горутины, что
создаёт URL. Если сервис падает между `INSERT INTO urls` и `kafka.Produce()` -
URL создан, но событие потеряно. Analytics-service никогда не узнает о новом URL.

### Решение
```
CREATE
  |
  |- INSERT INTO urls
  |- INSERT INTO outbox_events
  |- COMMIT (атомарно)
        |
        |
  outbox_events - status=pending
        |
        |
  poller (горутина)
    SELECT FOR UPDATE SKIP LOCKED
    - Kafka.Publish()
    - status=published
```

### Гарантии

- **At-least-once delivery**: если poller упадёт после публикации, но до
  `MarkPublished` - событие будет опубликовано повторно при следующем цикле.
  Analytics-service должен быть идемпотентным (дедупликация по event ID).
- **Нет потери при падении сервиса**: URL и событие создаются атомарно в одной
  транзакции PostgreSQL.
- **Горизонтальное масштабирование**: `SELECT FOR UPDATE SKIP LOCKED` позволяет
  нескольким инстансам poller'а работать параллельно без конфликтов.

### Kafka Topics

| Топик | События | Ключ партиции |
|---|---|---|
| `url-events` | url.created, url.deleted | short_code |
| `click-events` | url.clicked | short_code |

Ключ партиции `short_code` гарантирует, что все события одной ссылки
попадают в одну партицию - это обеспечивает упорядоченность при консьюминге
в analytics-service.