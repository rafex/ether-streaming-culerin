# runtime

## Nombre

runtime — detecta el runtime de contenedores

## Sinopsis

```
make runtime
make man TARGET=runtime
```

## Descripción

Delega en `container.sh --action runtime`: Podman si existe, si no Docker con aviso (regla 08).

## Ejemplos

```bash
make runtime
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
