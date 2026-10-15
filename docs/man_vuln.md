# vuln

## Nombre

vuln — vulnerabilidades conocidas alcanzables desde el código

## Sinopsis

```
make vuln
make man TARGET=vuln
```

## Descripción

Delega en `go.sh --goal vuln`: ejecuta `govulncheck` (versión fijada) sobre el
módulo. Falla solo si el código **llama** a una función vulnerable de una
dependencia; las de módulos que no se usan se listan pero no fallan. La
solución habitual es `go get <módulo>@<versión corregida> && go mod tidy`.

En CI corre como el job `vuln` (y cada lunes, para ver avisos nuevos sin
cambios en el código). No es un check obligatorio de `main`.

## Ejemplos

```bash
make vuln
```

## Véase también

`make lint`, `make test`
