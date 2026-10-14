---
title: Guía de inicio
tags: [guide, development]
---

# Guía de inicio

## Requisitos

| Herramienta | Versión | Para qué |
|-------------|---------|----------|
| Go | 1.27 | compilar y probar el puente |
| FFmpeg | 7.1+ con `libx264`, `aac`, `drawtext` | composición y publicación |
| GNU Make, just | — | puntos de entrada (regla 01) |
| Podman (o Docker) | — | CI en contenedor, MediaMTX, imagen del puente |
| Node 24 + el repo `ether-webrelay` al lado | — | probar con el frontend y `make e2e` |

```bash
just setup
make lint test
```

## Puente + webrelay en local

Tres terminales:

```bash
just run                      # puente en modo desarrollo: http://localhost:8090
```

```bash
cd ../ether-webrelay && just dev   # webrelay en http://localhost:5173
```

El `config.json` de desarrollo del webrelay ya apunta a
`ws://localhost:8090/signal`. En **modo desarrollo** las salas se crean al
entrar y cualquier token sirve: abre
`http://localhost:5173/join/demo?t=dev` en dos pestañas.

!!! warning "Puerto 8090"
    `just bridge-dev` del webrelay (el puente de pruebas en Node) usa el mismo
    puerto: detén uno antes de arrancar el otro.

## Emitir a un servidor RTMP local

```bash
just mediamtx                 # RTMP en localhost:1935, HLS en localhost:8888
just demo --live              # sala "demo" + destino MediaMTX + invitación, y al aire
```

`just demo` imprime el enlace de invitación. Entra con él (o con
`?t=dev`), y mira la emisión en `http://localhost:8888/live/demo`.

## Consola del anfitrión

`http://localhost:8090/console/` — token `dev` en modo desarrollo. Desde ahí:
crear salas, generar invitaciones, añadir destinos (YouTube/Twitch con su URL
de ingesta y clave), elegir la composición, poner o quitar de escena a cada
invitado, retirarlo y salir al aire.

## Probar con un destino real

1. En YouTube Studio → Emitir en directo, copia la URL del servidor
   (`rtmps://a.rtmp.youtube.com/live2`) y la clave de transmisión.
2. En la consola, añade el destino con esa URL y clave (la clave nunca vuelve
   a mostrarse completa ni llega al webrelay).
3. Pulsa **Salir al aire**; YouTube muestra la vista previa en unos segundos.

## E2E

```bash
make e2e    # Playwright del webrelay contra este puente (5 escenarios)
```
