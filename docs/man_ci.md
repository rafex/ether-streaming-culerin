# ci

## Nombre

ci — pipeline completo en contenedor

## Sinopsis

```
make ci
make man TARGET=ci
```

## Descripción

Construye la imagen de CI y ejecuta dentro `make install lint build coverage`. El árbol de trabajo entra por `tar` (sin bind-mounts: funciona con la VM de podman en macOS y volúmenes externos); la caché de módulos persiste en los volúmenes `ether-bridge-gomod` y `ether-bridge-gocache`. Es lo que ejecuta GitHub Actions.

## Ejemplos

```bash
make ci
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
