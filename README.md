# Negotiable Calendar

予定の詳細を公開せず、「いつ・どの程度・どう関われるか」だけを共有する、管理職向けのプライバシー重視カレンダーです。

現在は Issue 001（Project bootstrap）の実装段階です。Go API、React Web、PostgreSQL をローカルまたは Docker Compose で起動でき、lint・test・build を CI で検証します。画面内のスケジュールは UI シェル確認用のサンプルで、外部カレンダーとはまだ同期していません。

## 必要なもの

- Docker Desktop（推奨）
- または Go 1.25、Node.js 24、PostgreSQL 17

## Docker で起動

```bash
docker compose up --build
```

- Web: <http://localhost:3000>
- API health: <http://localhost:8080/healthz>
- API readiness: <http://localhost:8080/readyz>
- PostgreSQL: `localhost:5432`

終了するには `docker compose down` を実行します。データも消す場合だけ `docker compose down -v` を使用してください。

## 個別に起動

PostgreSQL を起動してから、API を実行します。

```powershell
$env:DATABASE_URL = "postgres://negotiable:negotiable_local@localhost:5432/negotiable_calendar?sslmode=disable"
$env:WEB_ORIGIN = "http://localhost:5173"
Set-Location apps/api
go run ./cmd/api
```

別のターミナルで Web を実行します。

```powershell
Set-Location web
npm install
npm run dev
```

## 品質チェック

```powershell
Set-Location apps/api
go test ./...
go vet ./...
go build ./cmd/api

Set-Location ../../web
npm run lint
npm test
npm run build
```

## 構成

```text
apps/api/       Go API
web/            React + Vite
docs/           要件・設計資料
.github/        CI
compose.yaml    ローカル統合環境
```

仕様の Source of Truth は [docs/requirements.md](docs/requirements.md) です。Private Event と組織向け Projection は異なる trust domain として扱い、組織向け API から予定詳細を参照できない境界を今後も維持します。
