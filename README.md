# Trip Service
## Требования

- Go 1.27+
- tripgoctl (https://github.com/course-go-autumn-2026/course-infra)
- make, psql, jq

## Запуск

```bash
tripgoctl cluster start
tripgoctl environment start
set -a; source .env; set +a
make migrate
make run
```

### Запуск в Docker
```bash
docker build -f deploy/Dockerfile -t trip-service:local .
set -a; source .env; set +a
docker run --rm -p 8080:8080 \
  -e HTTP_ADDR=:8080 \
  -e DATABASE_URL="$(echo "$DATABASE_URL" | sed 's/localhost/host.docker.internal/')" \
  -e LOG_LEVEL=info -e SHUTDOWN_TIMEOUT=10s \
  -e HTTP_READ_TIMEOUT=5s -e HTTP_READ_HEADER_TIMEOUT=2s \
  -e HTTP_WRITE_TIMEOUT=10s -e HTTP_IDLE_TIMEOUT=60s \
  -e DATABASE_MAX_CONNS=10 -e DATABASE_MIN_CONNS=2 \
  -e DATABASE_MAX_CONN_LIFETIME=30m \
  -e DATABASE_CONNECT_TIMEOUT=5s -e DATABASE_QUERY_TIMEOUT=3s \
  trip-service:local
```

## Переменные окружения
Все переменные перечислены в .env.example

# Лабараторная работа 1 
## Решения:
- Уровень изоляции — ReadCommitted. Это дефолт PostgreSQL и для данных задач его достаточно.
Запрет двух активных поездок у водителя держится не на уровне изоляции, и для завершения поездки тоже не нужен более строгий уровень: UPDATE атомарен сам по себе и если параллельный запрос уже перевел поездку в completed, второй UPDATE просто не найдет строку
- Менеджер транзакций: интерфейс TxManager с методом Do. Внутри Do открывает транзакцию через pool.BeginTx, кладёт ее в контекст через context.WithValue с приватным ключом (ctxKey — пустая структура, чтобы никто снаружи не залез по тому же ключу), затем вызывает fn с обогащенным контекстом. Если fn вернула nil, то Commit. Если ошибку, то Rollback и проброс. Если панику, то Rollback через defer + recover и проброс паники. Вложенный Do внутри Do не открывает вторую транзакцию, а переиспользует уже лежащую в контексте (проверка через TxFromContext). Репозиторий достает транзакцию из контекста через TxFromContext: если транзакция есть, то работает через нее, если нет, то через пул. Транзакция не передается аргументом метода репозитория, бизнес-код про pgx, транзакции и пулы не знает.
- Запрет двух активных поездок реализован с помощью частичного уникалього индекса в БД: индекс уникален только для строк со status='active', поэтому у водителя может быть только одна активная поездка.

## Что сделано:
- Структура репозитория по conventions.md: go mod init, структура cmd/, internal/, migrations/, contracts/, Makefile, .env, .gitignore.
- Код по OpenAPI сгенерирован через oapi-codegen: типы и серверный интерфейс под chi. Руками не писались.
- Миграции trips и trip_status_history с ограничениями из schema.md и уникальным индексом на активную поездку, up и down работают.
- Конфиг из env с валидацией обязательных переменных, туда же добавлены переменные таймаутов. Подключение к PostgreSQL через pgxpool, пинг на старте, при недоступной БД сервис не стартует.
- Менеджер транзакций: транзакция в контексте, вложенный Do переиспользует ее. Создание поездки пишет в две таблицы атомарно.
- Репозиторий на pgx + squirrel: SQL с $N, 23505 → ErrDriverBusy. Завершение защищено от гонки условием в UPDATE.
- Три ручки по контракту плюс /health и /ready. Коды и тело по контракту. Ошибки в application/problem+json с полем code.
- Сервер на net/http + chi, таймауты ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout (добавлены в конфиг из .env). Graceful shutdown по SIGINT/SIGTERM в пределах SHUTDOWN_TIMEOUT.

## Задачи со звёздочкой:
- Идемпотентность POST /api/v1/trips по заголовку Idempotency-Key. Первый запрос с ключом — 201, повтор с тем же ключом и телом — 200 и та же поездка, тот же ключ с другим телом — 409 idempotency_conflict. Без заголовка — обычные 201. Ключ и поездка пишутся в одной транзакции, TTL ключа — 24 часа, после этого запись не читается.
- Dockerfile с многостадийной сборкой: build на golang:1.27-alpine, итоговый образ на gcr.io/distroless/static-debian12:nonroot, без исходников и тулчейна, процесс от nonroot. Размер итогового образа — 4.93 МБ.

# Лабараторная работа 2

## Архитектура
Зависимости направлены внутрь: транспорт и репозитории - usecase - домен. Домен не знает ни про кого

- internal/domain/trip: модель поездки, переходы статусов, доменные ошибки, не импортирует инфраструктуру;
- internal/usecase/trip/{create,get,finish}: бизнес-логика, пакет на действие;
- internal/http/trip/{create,get,finish}: ручки; 
- internal/http/domainerror: единственное место, где доменная ошибка превращается в Problem; 
- internal/http/middleware: Recover - Tracing - Logging - Metrics;
- internal/repository/trip: SQL через pgx и squirrel;
- internal/infra/{postgres,telemetry}: пул с otelpgx, менеджер транзакций, OpenTelemetry и slog;
- internal/container: единственное место, где создаются зависимости и связываются друг с другом; 
- cmd/trip-service/main.go: только читает конфиг, запускает контейнер и ждет сигнала.

Интерфейсы (tripRepository, txManager, businessMetrics) объявлены неэкспортируемыми в contract.go того пакета, который их использует, а не рядом с реализацией. Так потребитель сам формулирует, что ему нужно, интерфейсы получаются узкими, а реализаций может быть несколько. Usecase не знает ни про pgx, ни про HTTP. 
Моки генерируются mockgen через go:generate в mocks_test.go рядом с кодом.
Правила слоев закреплены в сборке через depguard в .golangci.yml.

Отступление от раскладки: main.go лежит в cmd/trip-service, а не в cmd/service (путь из conventions.md).

## Тесты и линтер
```bash
make test               # unit, -race, печатает итоговое покрытие
make test-integration   # нужна БД: DATABASE_URL из .env
make lint
```

## Observability
Логи: slog, JSON, stdout, trace_id и span_id в каждой записи внутри спана.
Метрики и трейсы: OpenTelemetry, экспорт по OTLP в коллектор. http.route только шаблон маршрута.
