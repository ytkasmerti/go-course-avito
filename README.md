# Trip Service

## Требования
- Docker (запущенный), Go 1.27+, make, psql, jq
-  tripgoctl (https://github.com/course-go-autumn-2026/course-infra)
- macOS или Linux. На Windows работать через WSL 2 с запущенным Docker Desktop
Все команды выполняются из корня проекта. Адреса Jaeger, Grafana и Prometheus: `tripgoctl connect`.

## Локальный запуск
```bash
tripgoctl cluster start          # один раз на машине
tripgoctl environment start      # создает .env с адресами БД и коллектора
set -a; source .env.example; source .env; set +a
make migrate
make run
```
Остановка сервиса: `Ctrl+C`

## Запуск в Docker
```bash
tripgoctl cluster start          # один раз на машине
tripgoctl environment start
set -a; source .env; set +a
make migrate
docker build -f deploy/Dockerfile -t trip-service:local .
docker run --rm -p 8080:8080 \
  -e HTTP_ADDR=:8080 \
  -e DATABASE_URL="$(echo "$DATABASE_URL" | sed 's/localhost/host.docker.internal/')" \
  -e LOG_LEVEL=info -e SHUTDOWN_TIMEOUT=10s \
  -e HTTP_READ_TIMEOUT=5s -e HTTP_READ_HEADER_TIMEOUT=2s \
  -e HTTP_WRITE_TIMEOUT=10s -e HTTP_IDLE_TIMEOUT=60s \
  -e DATABASE_MAX_CONNS=10 -e DATABASE_MIN_CONNS=2 \
  -e DATABASE_MAX_CONN_LIFETIME=30m \
  -e DATABASE_CONNECT_TIMEOUT=5s -e DATABASE_QUERY_TIMEOUT=3s \
  -e OTEL_EXPORTER_OTLP_ENDPOINT="$(echo "$OTEL_EXPORTER_OTLP_ENDPOINT" | sed 's/localhost/host.docker.internal/')" \
  trip-service:local
```
Остановка сервиса: `Ctrl+C`

## Переменные окружения
Все переменные перечислены в `.env.example`. Адреса БД и коллектора приходят в `.env` от `tripgoctl`. При запуске в Docker остальные переменные передаются через `-e`.

# Лабораторная работа 1 
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
- Идемпотентность POST /api/v1/trips по заголовку Idempotency-Key. Первый запрос с ключом — 201, повтор с тем же ключом и телом — 200 и та же поездка, тот же ключ с другим телом — 409 idempotency_conflict. Без заголовка — обычные 201. Ключ и поездка пишутся в одной транзакции. TTL ключа — 24 часа. Просроченный ключ при повторном использовании атомарно заменяется, поэтому повтор после TTL создает новую поездку, а не получает ложный driver_busy.
- Dockerfile с многостадийной сборкой: build на golang:1.27-alpine, итоговый образ на gcr.io/distroless/static-debian12:nonroot, без исходников и тулчейна, процесс от nonroot. Размер итогового образа — 4.93 МБ.

## Доработки по замечаниям ревью
- Валидация тела POST /trips до обращения к БД: обязательные поля, ненулевые UUID, диапазоны координат, price >= 0, запрет неизвестных полей и данных после JSON. Всё это даёт 400 invalid_request. Ошибки разбора tripId и Idempotency-Key тоже отдаются как application/problem+json.
- Конфиг: все таймауты и время жизни соединения > 0, DATABASE_MAX_CONNS > 0, 0 <= DATABASE_MIN_CONNS <= DATABASE_MAX_CONNS; при ошибке сервис не стартует с указанием переменной.
- DATABASE_QUERY_TIMEOUT ограничивает каждый запрос репозитория, BEGIN/COMMIT и Ping в /ready.
- При завершении поездки запись active → completed пишется в trip_status_history в той же транзакции, что и UPDATE.

# Лабораторная работа 2
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
make test-integration   # нужны окружение, make migrate и загруженный .env (set -a; source .env; set +a)
make lint
```

## Observability
Логи: slog, JSON, stdout, trace_id и span_id в каждой записи внутри спана.
Метрики и трейсы: OpenTelemetry, экспорт по OTLP в коллектор. http.route только шаблон маршрута.

## Мутационное тестирование (задача со звездочкой)
Инструмент: gremlins.

```bash
gremlins unleash ./internal/domain/
gremlins unleash ./internal/usecase/
```

До добавления тестов:
- domain: Killed 1, Lived 0, Not covered 2 (mutator coverage 33,33%)
- usecase: Killed 10, Lived 0, Not covered 11 (mutator coverage 47,62%)

После:
- domain: Killed 17, Lived 0, Not covered 0 (100%)
- usecase: Killed 22, Lived 0, Not covered 1 (95,65%)

Оставшийся мутант: NOT COVERED usecase.go:18:27, "24 * time.Hour" в объявлении const. Объявление константы не содержит исполняемых statements, поэтому в профиле покрытия его нет и gremlins его не запускает. Значение проверяется тестом "TestDo_Idempotent_FirstRequest_SavesKeyFor24h".

## Доработки по замечаниям ревью
- Завершение поездки идет через доменный Trip.Finish: usecase проверяет переход active - completed в домене и сохраняет результат через SaveFinished. Защита от конкурентного завершения осталась в БД. Запись active - completed в trip_status_history пишется в той же транзакции, что и обновление trips: при ошибке записи истории статус тоже откатывается
- Валидация создания поездки: HTTP проверяет присутствие обязательных полей, включая координаты внутри точек, и конец тела (второй объект или мусор после JSON дают 400). Правила допустимости (ненулевые UUID, диапазоны координат, неотрицательная цена) перенесены в домен (domain/trip/validation.go) и вызываются из usecase, поэтому другой транспорт их не обойдет
- Идемпотентность: просроченный ключ атомарно заменяется, поэтому повтор после TTL создает новую поездку, а не дает ложный driver_busy. Повторное использование ключа после 24 часов покрыто интеграционным тестом
- Маршруты и разбор параметров обслуживает сгенерированный серверный адаптер (api.HandlerWithOptions, internal/http/server), вручную написанных маршрутов нет. Ошибки разбора tripId и Idempotency-Key возвращаются как application/problem+json с кодом invalid_request
- Причина внутренней ошибки логируется один раз, в domainerror.Write, с контекстом запроса, клиенту детали не отдаются
- Паники: Tracing, Logging и Metrics записывают результат в defer, поэтому при панике сохраняются запись о запросе, метрика длительности и error.type в спане (итоговый статус 500). Порядок middleware остался по заданию: Recover - Tracing - Logging - Metrics. Проверено тестом всей цепочки
- БД: на уровне пула поверх otelpgx добавлены атрибуты db.operation.name, db.collection.name и error.type и метрика db.client.operation.duration; экспорт проверен интеграционным тестом. DATABASE_QUERY_TIMEOUT ограничивает весь вызов, включая ожидание свободного соединения, а также BEGIN/COMMIT и Ping в /ready
- Конфиг: проверяются значения (таймауты и время жизни соединения > 0, max > 0, 0 <= min <= max), файл линтера переименован в .golangci.yml
- Новые замечания линтера исправлены, новых выживших и непокрытых мутантов в мутационном тестировании нет