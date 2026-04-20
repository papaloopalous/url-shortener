# auth-service

Сервис аутентификации и авторизации.

Выдаёт JWT access token и opaque refresh token, обеспечивает их ротацию,
обнаружение повторного использования токенов и валидацию через gRPC для
всех downstream-сервисов.

---

## Архитектура

Сервис построен по принципам **Clean Architecture** - зависимости направлены
строго внутрь: `controller - usecase - domain`. Инфраструктурный код (postgres,
redis, kafka) живёт в `adapters` и реализует интерфейсы из `domain/service`.

**Контекст запроса** прокидывается через все слои - от `r.Context()` в HTTP
контроллере до SQL-запроса в pgx и команды в Redis. Это обеспечивает:
- корректную иерархию трейсов в Jaeger (дочерние спаны)
- отмену in-flight операций при дисконнекте клиента
- соблюдение дедлайнов по всей цепочке

---

## API

### HTTP endpoints

| Метод | Путь | Описание |
|-------|------|----------|
| `POST` | `/auth/register` | Регистрация нового пользователя |
| `POST` | `/auth/login` | Аутентификация, получение токен-пары |
| `POST` | `/auth/refresh` | Ротация refresh token |
| `POST` | `/auth/logout` | Инвалидация сессии и access token |
| `GET`  | `/healthz` | Liveness probe для Kubernetes |
| `GET`  | `/metrics` | Scrape endpoint для Prometheus |

#### POST /auth/register

```json
// Запрос
{
  "email": "alice@example.com",
  "password": "Str0ngPass!"
}

// Ответ 201
{
  "access_token":  "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "550e8400-e29b-41d4-a716-446655440000",
  "expires_in":    900,
  "token_type":    "Bearer"
}
```

| Статус | Причина |
|--------|---------|
| `201` | Успешная регистрация |
| `409` | Email уже зарегистрирован |
| `422` | Не прошла валидация (короткий пароль, пустой email) |
| `400` | Невалидный JSON |

#### POST /auth/login

```json
// Запрос
{ "email": "alice@example.com", "password": "Str0ngPass!" }

// Ответ 200 - та же структура что и /register
```

| Статус | Причина |
|--------|---------|
| `200` | Успешный вход |
| `401` | Неверный email или пароль (намеренно одинаковое сообщение - защита от перебора) |

#### POST /auth/refresh

```json
// Запрос
{ "refresh_token": "550e8400-e29b-41d4-a716-446655440000" }

// Ответ 200 - новая токен-пара, старый refresh token инвалидируется
```

| Статус | Причина |
|--------|---------|
| `200` | Токен ротирован, выдана новая пара |
| `401` | Сессия не найдена, истекла или отозвана |
| `401` | Повторное использование уже ротированного токена (все сессии пользователя уничтожены) |

#### POST /auth/logout

```json
// Запрос
{
  "refresh_token":      "550e8400-...",
  "jti":                "uuid-of-access-token",
  "access_ttl_seconds": 847
}
```

| Статус | Причина |
|--------|---------|
| `204` | Успешно, тело ответа пустое |

Logout всегда возвращает `204` - не раскрывает состояние сессии.

### gRPC

```protobuf
service AuthService {
  rpc ValidateToken(ValidateTokenRequest) returns (ValidateTokenResponse);
}

message ValidateTokenRequest  { string access_token = 1; }
message ValidateTokenResponse { string user_id = 1; string jti = 2; int64 expires_at = 3; }
```

Вызывается API Gateway и любыми downstream-сервисами. Централизует проверку
JWT-подписи и чёрного списка JTI - остальные сервисы не знают секрет и не
ходят в Redis.

**gRPC статус коды:**

| Код | Причина |
|-----|---------|
| `OK` | Токен валиден, claims в ответе |
| `UNAUTHENTICATED` | Невалидная подпись, истёкший токен или JTI в чёрном списке |

**Fail-open при недоступности Redis:** если Redis не отвечает при проверке JTI,
сервис пропускает токен (логирует warning). Access token истечёт естественным
образом в течение своего TTL (15 минут). Это осознанный trade-off: доступность
важнее мгновенной инвалидации при сбое Redis.

---

## Конфигурация

| Переменная | Обязательная | По умолчанию | Описание |
|------------|:---:|-------------|----------|
| `APP_ENV` | | `development` | `development` - текстовые логи, `production` - JSON |
| `HTTP_ADDR` | | `:8081` | Адрес HTTP сервера |
| `GRPC_ADDR` | | `:9091` | Адрес gRPC сервера |
| `POSTGRES_DSN` | Да | - | DSN для подключения к PostgreSQL |
| `PG_MAX_CONNS` | | `20` | Максимум соединений в пуле pgx |
| `PG_MIN_CONNS` | | `2` | Минимум idle-соединений в пуле |
| `REDIS_ADDR` | Да | - | `host:port` для Redis |
| `REDIS_PASSWORD` | | `""` | Пароль Redis (пусто = без аутентификации) |
| `REDIS_DB` | | `0` | Номер базы данных Redis |
| `JWT_SECRET` | Да | - | Ключ подписи HS256, минимум 32 символа |
| `BCRYPT_COST` | | `12` | Стоимость bcrypt (4 в тестах, 12-14 в продакшне) |
| `OTLP_ENDPOINT` | | `localhost:4318` | OTLP/HTTP endpoint для Jaeger |

---

## Запуск локально

Сервисы после запуска:

| Сервис | URL |
|--------|-----|
| auth-service HTTP | http://localhost:8081 |
| auth-service gRPC | localhost:9091 |
| Jaeger UI | http://localhost:16686 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 (admin/admin) |
| Kibana | http://localhost:5601 |

### Примеры запросов

```bash
# Регистрация
curl -s -X POST http://localhost:8081/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","password":"Str0ngPass!"}' | jq

# Логин
curl -s -X POST http://localhost:8081/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","password":"Str0ngPass!"}' | jq

# Обновление токена
curl -s -X POST http://localhost:8081/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token_из_ответа>"}' | jq

# Проверка токена через gRPC (нужен grpcurl)
grpcurl -plaintext \
  -d '{"access_token":"<access_token>"}' \
  localhost:9091 auth.v1.AuthService/ValidateToken
```

### Makefile команды

```bash
make build            # компиляция бинарника в ./bin/
make run              # запуск сервиса локально
make test             # unit-тесты с race detector
make test-integration # integration-тесты (нужен Docker)
make test-coverage    # тесты + HTML отчёт о покрытии
make lint             # golangci-lint
make fmt              # gofmt + goimports
make proto            # регенерация pb.go из auth.proto
make mock             # регенерация gomock стабов
make migrate-up       # применить миграции (нужен POSTGRES_DSN)
make migrate-down     # откатить последнюю миграцию
make docker-build     # собрать Docker образ
```

---

## Тесты

### Виды тестов

| Слой | Файл | Тип | Что тестируется |
|------|------|-----|-----------------|
| Usecase | `internal/usecase/auth_test.go` | Unit (gomock) | Register, Login, Refresh с reuse detection, Logout |
| HTTP | `internal/controller/http/auth_test.go` | Unit (httptest) | Все роуты, коды ответов, валидация, маппинг ошибок |
| gRPC | `internal/controller/grpc/server_test.go` | Контрактный (bufconn) | ValidateToken, fail-open при Redis down, дедлайны |
| Postgres | `internal/adapters/db/postgres/repo_test.go` | Integration (testcontainers) | CRUD, ротация, DeleteExpired, дубликаты |
| Redis | `internal/adapters/cache/redis/token_cache_test.go` | Integration (testcontainers) | RevokeJTI, TTL истечение, изоляция ключей |

### Устройство тестов

**Unit-тесты** используют `gomock` - все зависимости (репозитории, кэш, token manager)
заменяются стабами. Тесты быстрые и не требуют инфраструктуры.

**Integration-тесты** помечены build-тегом `//go:build integration` и запускаются
только через `make test-integration`. Каждый пакет поднимает свой контейнер через
`testcontainers-go`, применяет миграции через goose, запускает тесты и убивает
контейнер. При отсутствии Docker тесты пропускаются с exit code 0 (не падают).

**Контрактные тесты** gRPC используют `google.golang.org/grpc/test/bufconn` -
in-memory транспорт без реального TCP. Это позволяет тестировать сериализацию,
middleware и обработку ошибок без поднятия настоящего gRPC сервера.

---

## CI/CD

GitHub Actions

**lint** - golangci-lint v1.59, таймаут 5 минут

**test-coverage** - `go test -race -count=1` с отчётом покрытия

**test-integration** - testcontainers на ubuntu-latest, `TESTCONTAINERS_RYUK_DISABLED=true`
для ускорения (Ryuk reaper не нужен в ephemeral CI среде)

**build** - статическая компиляция

**docker** - multi-stage build

---

## Observability

### Трейсинг (Jaeger)

Каждый HTTP-запрос и gRPC-вызов создаёт OTEL-спан. Спаны экспортируются
в Jaeger по OTLP/HTTP (`OTLP_ENDPOINT`).

HTTP middleware извлекает входящий `traceparent` заголовок (W3C TraceContext),
поэтому трейс от API Gateway продолжается внутри auth-service - вся цепочка
видна в одном Jaeger trace.

Для каждого usecase-метода создаётся дочерний спан с атрибутами:

```
AuthUsecase.Login
  |-- user.email = "alice@example.com"
  |-- user.id = "550e8400-..."    (только при успехе)
  |-- AuthUsecase.issueTokenPair
        |-- (создание сессии в PG)
```

Открыть Jaeger UI: http://localhost:16686 - выбрать сервис `auth-service`

### Логи (ELK)

Сервис пишет структурированный JSON в stdout. Каждая запись автоматически
обогащается `trace_id` и `span_id` из активного OTEL-спана - это позволяет
из Kibana перейти прямо к трейсу в Jaeger.

Цепочка: `stdout - Filebeat - Logstash (парсинг) - Elasticsearch - Kibana`

Пример лога:
```json
{
  "time":         "2026-01-09T11:17:44.123Z",
  "level":        "INFO",
  "msg":          "http request",
  "trace_id":     "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id":      "00f067aa0ba902b7",
  "method":       "POST",
  "path":         "/auth/login",
  "status":       200,
  "duration_ms":  47,
  "request_id":   "550e8400-e29b-41d4-a716-446655440000"
}
```

Открыть Kibana: http://localhost:5601 - создать index pattern `auth-service-*`

### Метрики (Prometheus + Grafana)

Scrape endpoint: `GET /metrics`

**HTTP метрики:**

| Метрика | Лейблы | Описание |
|---------|--------|----------|
| `auth_http_requests_total` | method, path, status | Счётчик запросов |
| `auth_http_request_duration_seconds` | method, path | Гистограмма латентности |

**gRPC метрики:**

| Метрика | Лейблы | Описание |
|---------|--------|----------|
| `auth_grpc_requests_total` | method, code | Счётчик gRPC вызовов |
| `auth_grpc_request_duration_seconds` | method | Гистограмма латентности |

**Бизнес-события:**

| Значение лейбла `event` | Описание |
|------------------------|----------|
| `register_ok` | Успешная регистрация |
| `register_duplicate` | Попытка регистрации с существующим email |
| `login_ok` | Успешный вход |
| `login_fail` | Неверные учётные данные |
| `refresh_ok` | Успешная ротация токена |
| `token_reuse` | Повторное использование ротированного токена |
| `logout` | Успешный выход |
| `token_valid_grpc` | Валидный токен через gRPC |
| `token_revoked_grpc` | Отозванный токен через gRPC |

**Ключевой алерт:** `auth_events_total{event="token_reuse"}` - спайк
означает попытку использования украденного refresh token. Все сессии
пользователя инвалидируются автоматически.

Открыть: http://localhost:3000 (admin/admin) - Dashboards - auth-service

---

## База данных

Миграции управляются через [goose].
Применяются автоматически при старте сервиса.

### Схема

```sql
-- Пользователи
CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Refresh-сессии
CREATE TABLE refresh_sessions (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL UNIQUE,
    user_agent  TEXT        NOT NULL DEFAULT '',
    ip_address  INET        NOT NULL DEFAULT '0.0.0.0',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,        -- now() + 30 дней
    revoked_at  TIMESTAMPTZ                  -- NULL = активна
);

-- Индекс для RevokeAllByUserID (reuse detection + logout all devices)
CREATE INDEX idx_refresh_sessions_user_id    ON refresh_sessions (user_id);

-- Индекс для Cleanup job: DELETE WHERE expires_at < now()
CREATE INDEX idx_refresh_sessions_expires_at ON refresh_sessions (expires_at)
    WHERE revoked_at IS NULL;
```

### Почему revoked_at, а не DELETE

При ротации токена старая запись **не удаляется**, а помечается `revoked_at`.
Это ключевое архитектурное решение: если клиент попытается снова использовать
уже ротированный токен, мы отличим "токен никогда не существовал" от "токен
уже был использован". Второй случай - сигнал кражи.

### Redis

Используется только для чёрного списка JTI при logout:

```
Ключ:  auth:revoked_jti:{jti}
TTL:   равен оставшемуся времени жизни access token
Тип:   String (значение "1")
```

Access token живёт 15 минут и является stateless JWT. Redis позволяет его
мгновенно отозвать при logout - ключ самоудаляется после TTL, очистка не нужна.

---

## Безопасность

**Refresh token** - opaque UUID (128 бит энтропии). Хранится только bcrypt-хеш
с `MinCost` (быстро - достаточно для UUID, не для пароля). Сырой токен
отдаётся клиенту один раз и больше нигде не сохраняется.

**Ротация токенов (RFC 6819)** - каждый refresh порождает новую пару и
инвалидирует старую сессию. Если старый токен используется повторно -
это сигнал компрометации, все сессии пользователя уничтожаются.

**Защита от перебора пользователей** - `ErrUserNotFound` и неверный пароль
возвращают одинаковый `ErrInvalidPassword`. Клиент не может определить,
существует ли email в системе.

**JTI чёрный список** - при logout access token добавляется в Redis с TTL
равным оставшемуся времени жизни. Gateway проверяет JTI при каждом запросе.

**Контекст запроса** - `context.Context` прокидывается через все слои.
Отмена запроса клиентом прерывает операции в БД и Redis - не расходуем
ресурсы на уже неактуальные запросы.