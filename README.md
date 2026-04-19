# gateway-service

API Gateway для платформы сокращения URL. Стоит перед `auth-service`, `shortener-service` и `analytics-service`. Не содержит бизнес-логики - только транспорт.

---

## Архитектура

```
Client
  |
  |
gateway-service :8080
  |- Rate Limit (Sliding Window / Redis)
  |- JWT Validation (gRPC - auth-service)
  |- Load Balancer (Least Connections)
  |
  |--- auth-service      :9090  (HTTP) / :9091 (gRPC)
  |--- shortener-service :8082
  |--- analytics-service :8083
```

---

## Маршруты

| Метод    | Путь            | Upstream    | JWT обязателен  |
|----------|-----------------|-------------|-----------------|
| `POST`   | `/auth/register`| auth        | Нет             |
| `POST`   | `/auth/login`   | auth        | Нет             |
| `POST`   | `/auth/refresh` | auth        | Нет             |
| `DELETE` | `/auth/logout`  | auth        | Да              |
| `POST`   | `/urls`         | shortener   | Да              |
| `GET`    | `/urls`         | shortener   | Да              |
| `DELETE` | `/urls`         | shortener   | Да              |
| `GET`    | `/{code}`       | shortener   | Нет             |
| `GET`    | `/stats/{code}` | analytics   | Нет             |
| `GET`    | `/healthz`      | gateway     | Нет (no RL)     |
| `GET`    | `/metrics`      | gateway     | Нет (no RL)     |

> Rate limit применяется ко всем маршрутам кроме `/healthz` и `/metrics`.

---

## Конфигурация

| Переменная              | По умолчанию       | Описание                                    |
|-------------------------|--------------------|---------------------------------------------|
| `APP_ENV`               | `development`      | Влияет на формат логов (text / JSON)        |
| `HTTP_ADDR`             | `:8080`            | Адрес HTTP сервера                          |
| `AUTH_GRPC_ADDR`        | -                  | Адрес gRPC сервера auth-service             |
| `AUTH_ADDRS`            | -                  | HTTP адреса auth-service (через запятую)    |
| `SHORTENER_ADDRS`       | -                  | HTTP адреса shortener-service               |
| `ANALYTICS_ADDRS`       | -                  | HTTP адреса analytics-service               |
| `REDIS_ADDR`            | `localhost:6379`   | Адрес Redis                                 |
| `REDIS_PASSWORD`        | -                  | Пароль Redis                                |
| `REDIS_DB`              | `3`                | Номер базы Redis                            |
| `RATE_LIMIT_REQUESTS`   | `100`              | Макс. запросов за окно                      |
| `RATE_LIMIT_WINDOW`     | `1m`               | Размер окна (парсится `time.ParseDuration`) |
| `RATE_LIMIT_ENABLED`    | `true`             | Включить/выключить rate limiting            |
| `HEALTHCHECK_INTERVAL`  | `10s`              | Интервал между health-проверками            |
| `HEALTHCHECK_TIMEOUT`   | `2s`               | Таймаут одной health-проверки               |
| `OTLP_ENDPOINT`         | `localhost:4318`   | OTLP/HTTP endpoint Jaeger                   |

Несколько инстансов одного сервиса передаются через запятую:

```
SHORTENER_ADDRS=http://localhost:8082,http://localhost:8083
```

---

## Тесты

```bash
# Unit-тесты (без Docker)
make test

# Integration-тесты (запускают Redis через testcontainers)
make test-integration

# Coverage
make test-coverage
```

Тест-покрытие включает:
- `internal/ratelimit` - integration, testcontainers Redis.
- `internal/balancer` - unit, `-race`.
- `internal/proxy` - unit, httptest upstream.
- `internal/controller/http` - unit, httptest, fake authClient.

---

## CI/CD

### GitHub Actions (`.github/workflows/ci.yml`)

Pipeline: **lint - test-unit - test-integration - build - docker**

---

## Observability

| Инструмент  | Адрес по умолчанию         |
|-------------|----------------------------|
| Jaeger UI   | http://localhost:16686     |
| Prometheus  | http://localhost:9090      |
| Grafana     | http://localhost:3000      |
| Kibana      | http://localhost:5601      |

### Метрики Prometheus

```
gateway_http_requests_total            {method, path, status}
gateway_http_request_duration_seconds  {method, path}
gateway_events_total                   {event}           - ratelimit_allowed/rejected/error
gateway_upstream_requests_total        {upstream}        - auth|shortener|analytics
gateway_balancer_active_conns          {upstream, addr}  - gauge текущих соединений
gateway_instance_healthy               {upstream, addr}  - gauge 1=healthy, 0=unhealthy
```

### Grafana Dashboard

1. **HTTP RED** - rate, 5xx error rate, latency p50/p95/p99.
2. **Rate Limit** - allowed/s, rejected/s, redis_error/s.
3. **Upstream Routing** - requests/s по каждому upstream.
4. **Least Connections** - active conns per instance (gauge).
5. **Instance Health** - таблица состояния инстансов + алерт при падении всех.

### Логи

Структурированные JSON-логи через `slog`. ELK: Filebeat собирает логи контейнера, Logstash парсит JSON, Elasticsearch хранит, Kibana отображает.

---

## Leaky Bucket Rate Limiter

### Алгоритм

Бакет ёмкостью `capacity` вытекает с постоянной скоростью `rate = capacity / window`. Состояние хранится в Redis одной строкой:

```
"leaky:{ip}" - "{count}:{unix_milli}"
```

При каждом запросе:

```
1. GET ключ - (count, last_ts)
2. leaked = (now - last_ts).Seconds() * rate_per_sec
3. count  = max(0, count - leaked)   // вытечка
4. if count >= capacity - 429
5. count++ - SET ключ EX capacity/rate
```

### Почему Leaky Bucket

Leaky Bucket гарантирует равномерный выходной поток - независимо от бурста входящих запросов скорость обработки ограничена сверху. Это лучше Sliding Window при сценариях с кратковременными пиками: бакет амортизирует их вместо жёсткого отрезания.

### Fail Open

Любая ошибка Redis - `WARN` в лог - запрос пропускается. Сервис не падает при недоступности Redis.

---

## Least Connections Load Balancer

### Алгоритм

Перед каждым запросом `Next()` проходит по всем здоровым инстансам и выбирает тот, у которого наименьшее значение `activeConns`. Счётчик инкрементируется атомарно (`sync/atomic`) перед отдачей инстанса и декрементируется через `defer inst.Done()` после завершения запроса.

### Сравнение с Round-Robin

Round-Robin предполагает, что все запросы обрабатываются одинаковое время. Если один upstream медленнее (например, выполняет тяжёлый запрос), Round-Robin всё равно шлёт ему запросы. Least Connections учитывает реальную нагрузку и направляет новые запросы к наименее занятому инстансу.

### Health Checks

Отдельная горутина на каждый upstream пул периодически опрашивает `GET /healthz`. Инстансы помечаются `healthy=0/1` через атомарный флаг. `Next()` пропускает нездоровые инстансы. При полной недоступности пула - 503.

Логирование только при смене статуса (healthy-unhealthy и наоборот) - чтобы не засорять лог повторяющимися событиями.