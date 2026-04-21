# 🔗 URL Shortener Platform

Микросервисная платформа сокращения URL, построенная на Go. Проект охватывает полный цикл от аутентификации до аналитики и демонстрирует применение production-ready паттернов: Transactional Outbox, Inbox deduplication, GCRA rate limiting, Least Connections балансировка и распределённая трассировка.

---

## Архитектура

```
client
  -- gateway-service       (rate limit, JWT validation, load balancing)
       -- auth-service      (HTTP :8081, gRPC :9091)  --  postgres, redis
       -- shortener-service (HTTP :8082)               --  postgres, redis, kafka
       -- analytics-service (HTTP :8083)               --  postgres, redis, kafka
```

Каждый сервис следует принципам **Clean Architecture**: зависимости направлены строго внутрь (`controller - usecase - domain`), инфраструктурный код живёт в `adapters` и реализует интерфейсы из `domain`.

---

## Сервисы

### auth-service

Отвечает за регистрацию, вход, ротацию токенов и их валидацию.

- **JWT** (HS256, 15 минут) + **opaque refresh token** (UUID, 30 дней)
- Ротация токенов по **RFC 6819**: повторное использование ротированного токена - сигнал компрометации, все сессии пользователя уничтожаются немедленно
- **gRPC endpoint** `ValidateToken` для downstream-сервисов - централизованная проверка подписи и JTI-блэклиста, остальные сервисы не знают JWT-секрет
- **JTI blacklist** в Redis с TTL = оставшееся время жизни access token - мгновенный logout без stateful хранилища
- Защита от user enumeration: `ErrUserNotFound` и неверный пароль возвращают одинаковый ответ

### shortener-service

Создаёт короткие ссылки, выполняет редиректы и публикует события в Kafka.

- Генерация **7-символьных Base62** кодов с автоматическим retry при коллизии
- **Redis cache** (TTL 24ч) на горячем пути редиректа - БД читается только при cache miss
- **Soft delete**: ссылки помечаются `deleted_at`, физическое удаление делает отдельный cleanup-job
- **Transactional Outbox**: URL и outbox-событие создаются в одной транзакции PostgreSQL - гарантия доставки в Kafka даже при падении сервиса
- Ownership check на DELETE: чужие коды молча игнорируются

### analytics-service

Консьюмит события кликов из Kafka и предоставляет агрегированную статистику.

- **Inbox deduplication** (exactly-once обработка при at-least-once доставке Kafka): `INSERT INTO inbox_events ON CONFLICT DO NOTHING` в той же транзакции, что и запись клика
- **Worker pool** с конфигурируемым параллелизмом (default: 10 горутин)
- **Redis cache** статистики (TTL 5 минут) + инвалидация при получении нового клика
- Агрегация: total clicks, unique IPs, top countries, last click at

### gateway-service

API Gateway - единая точка входа без бизнес-логики.

- **GCRA Rate Limiter** (Leaky Bucket) в Redis: целочисленная арифметика в миллисекундах, без float-дрейфа. Атомарность через `WATCH + TxPipeline`
- **Least Connections Load Balancer**: атомарные счётчики (`sync/atomic`), направляет трафик к наименее нагруженному инстансу
- **Health checks**: фоновые горутины проверяют `/healthz` каждые 10с, нездоровые инстансы исключаются из ротации
- **JWT validation** через gRPC вызов к auth-service на каждый защищённый запрос
- Fail open при недоступности Redis: запросы пропускаются с `WARN` в лог

---

## Технологический стек

| Категория | Технологии |
|---|---|
| **Язык** | Go 1.22 |
| **HTTP** | net/http + chi router |
| **gRPC** | google.golang.org/grpc, protobuf |
| **База данных** | PostgreSQL (pgx/v5), миграции через goose |
| **Кэш** | Redis (go-redis/v9) |
| **Брокер** | Apache Kafka (segmentio/kafka-go) |
| **Трейсинг** | OpenTelemetry + Jaeger (OTLP/HTTP) |
| **Метрики** | Prometheus + Grafana |
| **Логи** | slog (JSON) - Filebeat - Logstash - Elasticsearch - Kibana |
| **Тесты** | gomock, testcontainers-go, httptest, bufconn |
| **CI/CD** | GitHub Actions |
| **Контейнеризация** | Docker, Docker Compose |

---

## Ключевые паттерны

### Transactional Outbox (shortener-service)

Решает проблему dual write: URL и событие для Kafka записываются **атомарно** в одной транзакции PostgreSQL. Отдельная горутина-poller читает `outbox_events` через `SELECT FOR UPDATE SKIP LOCKED` (поддерживает горизонтальное масштабирование) и публикует их в Kafka.

```
INSERT urls + INSERT outbox_events - COMMIT - poller - Kafka.Publish - status=published
```

### Inbox Deduplication (analytics-service)

Kafka гарантирует at-least-once доставку. Дедупликация реализована через таблицу `inbox_events` с `ON CONFLICT DO NOTHING` - идентификатор события из outbox проверяется в той же транзакции, что и запись клика. Дублирующееся сообщение пропускается, не влияя на статистику.

### GCRA Rate Limiting (gateway-service)

Generic Cell Rate Algorithm хранит в Redis одно целое число (TAT - Theoretical Arrival Time). Гарантирует равномерный поток запросов, амортизирует кратковременные пики, лишён float-дрейфа классического Leaky Bucket.

### Refresh Token Rotation + Reuse Detection (auth-service)

При ротации токена старая сессия **не удаляется**, а помечается `revoked_at`. Повторное использование ротированного токена отличается от несуществующего - это сигнал кражи: все сессии пользователя уничтожаются мгновенно.

---

## API

Все запросы идут через gateway-service на порту `:8080`.

### Аутентификация

| Метод | Путь | Описание | Auth |
|---|---|---|---|
| `POST` | `/auth/register` | Регистрация | - |
| `POST` | `/auth/login` | Вход | - |
| `POST` | `/auth/refresh` | Ротация refresh token | - |
| `DELETE` | `/auth/logout` | Инвалидация сессии | Да |

### Ссылки

| Метод | Путь | Описание | Auth |
|---|---|---|---|
| `POST` | `/urls` | Создать короткую ссылку | Да |
| `GET` | `/urls` | Список своих ссылок | Да |
| `DELETE` | `/urls` | Batch soft delete | Да |
| `GET` | `/{code}` | Редирект | - |

### Аналитика

| Метод | Путь | Описание | Auth |
|---|---|---|---|
| `GET` | `/stats/{code}` | Статистика по ссылке | - |

**Пример ответа `/stats/{code}`:**
```json
{
  "short_code":   "aB3xK7z",
  "total_clicks": 1542,
  "unique_ips":   876,
  "top_countries": [
    { "country": "RU", "clicks": 900 },
    { "country": "US", "clicks": 400 }
  ],
  "last_click_at": "2026-04-14T10:30:00Z"
}
```

---

## Observability

### Распределённая трассировка

Все сервисы используют OpenTelemetry с OTLP/HTTP экспортёром. Заголовок `traceparent` (W3C TraceContext) прокидывается через gateway - auth/shortener/analytics, поэтому вся цепочка запроса видна в **одном Jaeger trace**.

Ключевые spans с атрибутами:
- `AuthUsecase.Login` - `user.email`, `user.id`
- `URLUsecase.Redirect` - `url.short_code`, `url.cache_hit`
- `StatsUsecase.GetStats` - `stats.short_code`, `stats.cache_hit`
- `Pool.process` - `click.short_code`

### Метрики и дашборды

Каждый сервис экспортирует `/metrics` в формате Prometheus. Grafana включает 4 преднастроенных дашборда:

| Дашборд | Секции |
|---|---|
| **gateway** | HTTP RED, Rate Limit, Upstream Routing, Instance Health |
| **auth** | HTTP RED, gRPC latency, Business events (register/login/reuse) |
| **shortener** | HTTP RED, Redirect rate, Cache hit ratio, Outbox pipeline |
| **analytics** | HTTP RED, Worker Pool throughput, Cache hit ratio |

**Ключевой алерт:** `auth_events_total{event="token_reuse"}` - спайк означает попытку использования украденного refresh token.

### Логи

Все сервисы пишут структурированный JSON в stdout. Каждая запись обогащается `trace_id` и `span_id` - из Kibana можно перейти прямо к трейсу в Jaeger.

```
stdout - Filebeat - Logstash - Elasticsearch - Kibana
```

---

## Тестирование

Каждый сервис покрыт двумя видами тестов.

### Unit-тесты (gomock)

Все внешние зависимости заменяются мок-стабами. Тесты быстрые, не требуют Docker и запускаются с `-race` detector. Покрывают usecase-логику, HTTP/gRPC контроллеры и worker pool.

### Integration-тесты (testcontainers-go)

Помечены build-тегом `integration`. Каждый пакет поднимает реальный контейнер (PostgreSQL или Redis), применяет миграции через goose и запускает тесты. При отсутствии Docker - пропускаются с exit code 0.

Покрывают: атомарность транзакций, deduplication, TTL expiry, Kafka producer/consumer, ownership checks.

### gRPC контрактные тесты

Используют `bufconn` (in-memory транспорт) - тестируют сериализацию, middleware и обработку ошибок без реального TCP.

```bash
make test                 # unit-тесты
make test-integration     # integration-тесты (нужен Docker)
make test-coverage        # отчёт о покрытии
```

---

## CI/CD

Каждый сервис имеет собственный GitHub Actions pipeline на своей ветке:

```
lint - test-unit - test-integration - build - docker
```

---

## Будущие улучшения

Cleanup service - сейчас удалённые ссылки и истёкшие сессии остаются в БД с пометкой. Хочется добавить отдельный сервис, который будет по расписанию физически чистить эти строки

Notification service - новый сервис с WebSocket-подключением, чтобы статистика обновлялась в браузере в реальном времени без перезагрузки страницы

Kubernetes - сейчас всё запускается через Docker Compose, следующий шаг - деплоить в кластер