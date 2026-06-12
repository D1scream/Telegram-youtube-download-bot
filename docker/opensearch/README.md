# OpenSearch

Индекс `uk_rf` — статьи УК РФ, гибридный поиск (BM25 + kNN).

```powershell
docker compose -f docker/opensearch/docker-compose.yml up -d
go run ./docker/opensearch/indexer
```

Первый запуск TEI (скачать модель в `.cache/`):

```powershell
powershell -File docker/opensearch/download.ps1
docker compose -f docker/opensearch/docker-compose.yml up -d embeddings
go run ./docker/opensearch/indexer
```

Индексатор — отдельный Go-модуль в `indexer/`, не связан с ботом.  
Флаги: `-uk`, `-config-dir`, `-opensearch-url`, `-embeddings-url`.  
Переменные окружения: `OPENSEARCH_URL`, `EMBEDDINGS_URL` (из `.env` в корне репо).

Остановка: `docker compose -f docker/opensearch/docker-compose.yml down`
