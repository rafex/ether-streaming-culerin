# docs

## Nombre

docs — genera el sitio de documentación con MkDocs

## Sinopsis

```
make docs [SITE_DIR=site]
make man TARGET=docs
```

## Descripción

Delega en `helpers/shell/docs.sh --goal build`. Usa `mkdocs` si está instalado; si no, `uvx` con `.config/mkdocs/requirements.txt`. Los diagramas son MermaidJS embebido.

## Ejemplos

```bash
make docs
```

## Véase también

- `spec-native/COMMANDS.md` (índice de comandos del proyecto)
- [Regla 01: Herramientas de Construcción](https://github.com/rafex/ether-my-best-practice/blob/main/rules/01-build-tooling.md)
