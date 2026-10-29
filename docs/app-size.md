## App size: `webapp`
- Language: **Go** `webapp/go` (go.mod) · 8 files · **2,088 lines** · other dirs: payment_mock (Go)
- Largest files: app_handlers.go 1,006 · chair_handlers.go 338 · owner_handlers.go 241 · main.go 184 · payment_gateway.go 97 · models.go 87 · middlewares.go 82 · internal_handlers.go 53
- Routes: **18** (POST 11, GET 7) · handler funcs: 20 · router: chi + net/http patterns
- Routes registered in: main.go 18
- OpenAPI: `webapp/openapi.yaml` (15 paths)
- Tables: **10** in `sql/1-schema.sql` · initial rows ≈ 56 (INSERT tuples in 3 .sql/.sql.gz) · **not in repo** (referenced by init*.sh): 3-initial-data.sql.gz

| table | cols | idx (PK+keys) | initial rows |
|---|---:|---:|---:|
| chair_models | 2 | 1 | 55 |
| settings | 2 | 1 | 1 |
| chair_locations | 5 | 1 | - |
| chairs | 8 | 1 | - |
| coupons | 5 | 1 | - |
| owners | 6 | 4 | - |
| payment_tokens | 3 | 1 | - |
| ride_statuses | 6 | 1 | - |
| rides | 10 | 1 | - |
| users | 9 | 4 | - |

- Components: docker: payment_mock/Dockerfile · **exec**: go/main.go:131
