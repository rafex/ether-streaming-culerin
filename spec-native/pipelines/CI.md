# CI.md

Integración continua del proyecto.

### Plataforma

- Plataforma de CI: GitHub Actions (envoltura; la lógica vive en `make`/helpers — regla 06).
- Archivo de configuración: `.github/workflows/ci.yml`.
- Donde ver resultados: pestaña Actions del repositorio.

### Triggers

| Evento | Pipeline que se ejecuta |
| --- | --- |
| Pull request abierto | `build-test` + `e2e` |
| Push a `main` / `develop` | `build-test` + `e2e` |
| Release publicado | — (ver `CD.md`) |

### Gates obligatorios

| Gate | Herramienta | Comando local |
| --- | --- | --- |
| Lint | gofmt + go vet | `make lint` |
| Tests (unit + integración + arquitectura) | go test -race | `make test` |
| Cobertura ≥ 80 % | go test -coverpkg | `make coverage` |
| Build | go build | `make build` |
| Pipeline en contenedor | podman + `containers/ci/Containerfile` | `make ci` |
| Imagen del puente | podman | `make app-image` |
| E2E con el webrelay | Playwright (repo `ether-webrelay`, rama main) | `make e2e` |

### Gates opcionales o informativos

| Gate | Herramienta | Observaciones |
| --- | --- | --- |
| Análisis estático extra | staticcheck | se ejecuta en `make lint` si está instalado |
| Secretos | gitleaks / trufflehog | hooks `pre-commit` / `pre-push` si están instalados |
