# scan

## Nombre

scan — vulnerabilidades corregibles en la imagen del puente

## Sinopsis

```
make scan
make man TARGET=scan
```

## Descripción

Delega en `container.sh --action scan`: guarda la imagen del puente
(`ether-bridge:local`, la construye si no existe) y la analiza con Trivy 0.74.0
en un contenedor. Cubre los paquetes de Debian y las dependencias del binario Go
(incluida la biblioteca estándar con la que se compiló). Falla (`exit 1`) si hay
alguna vulnerabilidad **HIGH o CRITICAL que ya tenga versión corregida**; las que
aún no se pueden arreglar no bloquean.

Las soluciones habituales: subir la base del `Containerfile` (tag y digest), o
`go get <módulo>@<versión corregida>`. La imagen hace `apt-get upgrade` al
construirse, así que reconstruir recoge los parches de Debian publicados después
de la base.

En CI es el job `scan` (y corre cada lunes). No es un check obligatorio de `main`.

## Véase también

`make app-image`, `make vuln`
