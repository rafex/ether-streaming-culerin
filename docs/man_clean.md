# clean

## Nombre

clean — limpia artefactos generados

## Sinopsis

```
make clean
make man TARGET=clean
```

## Descripción

Delega en `clean.sh` con las rutas `site`, `MODULE/bin` y `MODULE/coverage.out`. Rechaza rutas peligrosas (`/`, `..`, absolutas fuera del workspace).

## Ejemplos

```bash
make clean
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
