# analytics-service

Микросервис аналитики кликов.

Консьюмит события кликов из Kafka (публикует `shortener-service`), сохраняет их
в PostgreSQL с **Inbox deduplication** (exactly-once обработка при at-least-once
доставке), агрегирует статистику и отдаёт её через REST API с Redis-кэшем.

---

## Архитектура

```
Kafka (click-events)
        |
        |
  Worker Pool (N горутин)
        |
        |-- InboxRepo.SaveWithClick (tx: inbox_events + click_events)
        |                    ↑
        |         ON CONFLICT DO NOTHING (dedup)
        |
        |-- StatsCache.Invalidate (Redis)

Client - GET /stats/{code}
              |
              |-- Redis cache hit  - Stats (TTL 5m)
              |-- Redis cache miss - PG aggregation - Redis set
```

Сервис построен по принципам **Clean Architecture**:

```
controller/http   -  HTTP запросы, DTOs
usecase           -  бизнес-логика (GetStats)
domain            -  сущности, интерфейсы-порты
adapters          -  PostgreSQL, Redis, Kafka
worker            -  worker pool (Kafka consumer + inbox dedup)
```

---

## API

### `GET /stats/{code}` - статистика по короткой ссылке (публичный)

```bash
curl http://localhost:8083/stats/aB3xK7z
```

**Ответ 200:**
```json
{
  "short_code":   "aB3xK7z",
  "total_clicks": 1542,
  "unique_ips":   876,
  "top_countries": [
    { "country": "RU", "clicks": 900 },
    { "country": "US", "clicks": 400 },
    { "country": "DE", "clicks": 242 }
  ],
  "last_click_at": "2026-04-14T10:30:00Z"
}
```

Если кликов ещё не было - возвращает `200` с `total_clicks: 0`.

### `GET /healthz` - liveness probe

```bash
curl http://localhost:8083/healthz
# - 200 ok
```

### `GET /metrics` - Prometheus scrape

```bash
curl http://localhost:8083/metrics
```

---

## Конфигурация

| Переменная | Описание | Пример |
|---|---|---|
| `APP_ENV` | Режим (development/production) | `development` |
| `HTTP_ADDR` | Адрес HTTP сервера | `:8083` |
| `POSTGRES_DSN` | DSN PostgreSQL | `postgres://user:pass@host/db` |
| `PG_MAX_CONNS` | Макс. соединений в пуле | `20` |
| `PG_MIN_CONNS` | Мин. соединений в пуле | `2` |
| `REDIS_ADDR` | Адрес Redis | `localhost:6379` |
| `REDIS_PASSWORD` | Пароль Redis | `` |
| `REDIS_DB` | Номер БД Redis | `2` |
| `KAFKA_BROKERS` | Список брокеров | `localhost:9092` |
| `KAFKA_CLICK_EVENTS_TOPIC` | Топик кликов | `click-events` |
| `KAFKA_GROUP_ID` | Consumer group ID | `analytics-service` |
| `WORKER_CONCURRENCY` | Параллельных worker горутин | `10` |
| `STATS_CACHE_TTL` | TTL кэша статистики | `5m` |
| `OTLP_ENDPOINT` | Адрес Jaeger OTLP/HTTP | `localhost:4318` |

---

## Запуск локально

### Через Docker Compose

```bash
cp .env.example .env
make docker-up          # или: docker compose up -d --build
```

**Доступные UI:**
- Grafana: http://localhost:3001 (admin/admin)
- Jaeger: http://localhost:16687
- Prometheus: http://localhost:9091
- Kibana: http://localhost:5602

### Локально без Docker

```bash
cp .env.example .env
# Отредактировать адреса в .env

make build
./bin/analytics-service
```

---

## Тесты

### Unit тесты

`usecase/stats_test.go` - 5 сценариев через gomock:
- Cache hit - в БД не идём
- Cache miss - PG aggregation - populate cache
- Cache set fail (best-effort) - результат всё равно возвращается
- Пустая статистика (0 кликов)
- Ошибка БД - 500

`worker/pool_test.go` - 5 сценариев через gomock + fakeConsumer:
- Успешная обработка - SaveWithClick + Invalidate
- Дубликат события - ErrDuplicateEvent - skip, без Invalidate
- Ошибка SaveWithClick - метрика click_failed
- Невалидный JSON - skip
- Graceful shutdown - pool.Run возвращается при cancel ctx

`controller/http/stats_test.go` - 4 сценария через httptest:
- GET /stats/{code} - 200 с корректными данными
- Пустая статистика - 200 с zero values
- Ошибка БД - 500
- GET /healthz - 200

### Integration тесты (testcontainers, один контейнер на пакет через TestMain)

`adapters/db/postgres/repo_test.go`:
- SaveWithClick: успех + проверка GetStats
- SaveWithClick: дубликат - ErrDuplicateEvent
- GetStats: пустая таблица - 0 кликов
- GetStats: агрегация (3 клика, 2 IP, 2 страны)

`adapters/cache/redis/stats_cache_test.go`:
- Get/Set/Invalidate
- TTL expiry (100ms)

---

## CI/CD

### GitHub Actions

```
lint - test-unit - test-integration - build - docker
```

---

## Observability

### Трейсинг (Jaeger)

Трейсы создаются в:
- `StatsUsecase.GetStats` - атрибуты: `stats.short_code`, `stats.cache_hit`
- `Pool.process` - атрибуты: `click.short_code`

### Метрики (Prometheus + Grafana)

| Метрика | Тип | Labels |
|---|---|---|
| `analytics_http_requests_total` | Counter | method, path, status |
| `analytics_http_request_duration_seconds` | Histogram | method, path |
| `analytics_events_total` | Counter | event |
| `analytics_worker_active` | Gauge | - |

Значения `event`: `click_processed`, `click_duplicate`, `click_failed`, `cache_hit`, `cache_miss`

**Grafana dashboard** (`analytics-service.json`) включает 3 секции:
1. HTTP RED (rate, errors, latency p50/p95/p99)
2. Worker Pool: active gauge + click throughput (processed/duplicate/failed/s)
3. Cache: hit ratio + hit vs miss rate

### Логи (ELK)

JSON логи - Filebeat - Logstash - Elasticsearch - Kibana.
Индекс: `analytics-service-YYYY.MM.dd`

---

## База данных

### Схема

```sql
-- Основная таблица событий кликов
click_events (
    id          UUID PRIMARY KEY,
    short_code  TEXT NOT NULL,
    ip          TEXT,          -- NULL если недоступен
    user_agent  TEXT,
    referer     TEXT,
    country     TEXT,          -- NULL если GeoIP не настроен
    clicked_at  TIMESTAMPTZ NOT NULL
)

-- Таблица для inbox deduplication
inbox_events (
    event_id    UUID PRIMARY KEY,  -- = UUID из outbox_events shortener-service
    created_at  TIMESTAMPTZ NOT NULL
)
```

### Индексы

```sql
-- Горячий путь: COUNT(*) + MAX(clicked_at) по short_code
CREATE INDEX idx_click_events_short_code ON click_events (short_code, clicked_at DESC);

-- GROUP BY country WHERE short_code = $1
CREATE INDEX idx_click_events_country ON click_events (short_code, country)
    WHERE country IS NOT NULL;
```

Индекс `inbox_events.event_id` - PRIMARY KEY (автоматически).

---

## Inbox паттерн

### Проблема

Kafka гарантирует **at-least-once** доставку. Одно и то же событие клика может
прийти дважды (после рестарта сервиса, повторного rebalance и т.д.). Без
дедупликации статистика будет завышена.

### Решение

```
Kafka message - FetchMessage()
                    |
                    |
         BEGIN TRANSACTION
                    |
                    |
    INSERT INTO inbox_events (event_id)
    ON CONFLICT DO NOTHING
                    |
            |-------|--------|
       0 rows affected    1 row affected
            |                    |
       ErrDuplicateEvent    INSERT INTO click_events
       (skip, commit)            |
                                 |
                            COMMIT
                                 |
                                 |
                       CommitMessages (Kafka offset)
```

**Ключевой момент порядка операций:**
1. `INSERT INTO inbox_events` (idempotency key)
2. `INSERT INTO click_events` (сам клик)
3. `COMMIT` транзакции
4. `CommitMessages` (Kafka offset)

Если сервис упадёт между шагом 3 и 4 - Kafka повторно доставит сообщение,
но `ON CONFLICT DO NOTHING` его проигнорирует.

---

## Worker Pool

Pool читает сообщения из Kafka и обрабатывает их параллельно:

Конфигурируется через `WORKER_CONCURRENCY` (default: 10)