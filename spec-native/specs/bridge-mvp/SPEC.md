# SPEC — bridge-mvp

```toml
artifact_type = "spec"
id            = "SPEC-0001"
state         = "done"
owner         = "rafex"
created_at    = "2026-10-13"
updated_at    = "2026-10-14"
replaces      = "none"
related_tasks       = ["TASK-0001", "TASK-0002", "TASK-0003", "TASK-0004", "TASK-0005", "TASK-0006", "TASK-0007", "TASK-0008", "TASK-0009"]
related_decisions   = ["DEC-0001", "DEC-0002", "DEC-0003", "DEC-0004", "DEC-0005", "DEC-0006"]
artifacts           = ["source/backend/go/bridge/*", "containers/*", "helpers/*", "docs/*"]
validation          = ["make ci", "make e2e", "prueba manual con MediaMTX", "prueba manual con un destino real"]
```

## Resumen

Puente que alberga la videollamada del webrelay (señalización v2 + SFU Pion),
compone la sala con FFmpeg y la publica en uno o varios destinos RTMP(S),
controlado por una API REST y una consola web del anfitrión.

## Problema

El webrelay ya existe y su contrato está probado contra un puente de
desarrollo en Node que no publica nada. Falta el puente real. Ver
`../../PRODUCT.md`.

## Objetivo

Que un anfitrión cree una sala, invite con un enlace firmado, los invitados se
vean y se oigan desde el webrelay, y la sala compuesta llegue a sus canales.

## Alcance

### Incluye

- Señalización v2 completa (`room.peek`…`leave`), reanudación, códigos de
  cierre, validación de origen/subprotocolo/tamaño.
- SFU: publish/subscribe, `tracks`, ICE trickle con cola, PLI, puerto UDP
  único, IP anunciada.
- Composición: mosaico/foco/orador, tarjetas, rótulos, mezcla de audio,
  detector de pistas detenidas, empalme sin cortes, publicación en `tee`.
- API REST + consola: salas, invitaciones HMAC, destinos con clave enmascarada,
  escena, expulsar, salir al aire, vista del layout.
- Modo desarrollo compatible con el E2E del webrelay; MediaMTX para pruebas.
- Tooling ether-rules (make/just/helpers), contenedores, CI, docs, SpecNative.

### Excluye

- Persistencia, multi-instancia, grabación como producto, simulcast.

## Requisitos funcionales

1. RF1 — `room.peek` con invitación válida devuelve `room.info`; inválida,
   caducada o de otra sala → `invalid-token` fatal (cierre 4401).
2. RF2 — `join` responde `welcome` (antes de cualquier mensaje del SFU) con
   perfil y participantes; los demás reciben `participant.joined`.
3. RF3 — Las pistas publicadas llegan a todos los demás (`tracks` +
   `subscribe.offer`), nunca con una oferta vacía.
4. RF4 — `self.update` se difunde; `leave`/cierre quita al participante (tras
   20 s de gracia si fue una caída; `resume` lo recupera).
5. RF5 — El anfitrión crea salas, emite invitaciones, gestiona destinos y la
   escena, expulsa y cierra salas por API y consola.
6. RF6 — Salir al aire publica la composición en todos los destinos activos;
   la composición sigue la sala (entradas, salidas, pantalla, cámaras).
7. RF7 — Ninguna respuesta de la API ni mensaje al webrelay contiene una clave
   de emisión completa.

## Requisitos no funcionales

- Hexágono comprobado por test; cobertura ≥ 80 %; `go test -race` limpio.
- Un cambio de layout no reabre las conexiones RTMP(S).
- Contenedor sin root, bases por digest, secretos solo por entorno.

## Criterios de aceptación

- **CA1** — `make e2e` (5 escenarios del webrelay) pasa contra el puente.
- **CA2** — Con MediaMTX, una emisión de ~45 s con entradas, pantalla
  compartida, cámara apagada y salidas decodifica sin errores y sin
  reconexión RTMP.
- **CA3** — Una invitación emitida por la API abre el lobby; manipulada o de
  otra sala es rechazada.
- **CA4** — La consola permite el ciclo completo (sala → destino → invitación
  → al aire → detener → cerrar).
- **CA5** — `make ci` pasa en contenedor; la imagen arranca y responde
  `/healthz`.
- **CA6** — Prueba manual con un destino real (YouTube o Twitch).

## Dependencias y riesgos

- FFmpeg no admite cambiar entradas en caliente → una ejecución por layout +
  empalme (DEC-0004). Hueco de 2.5–4 s cuando una entrada se detiene.
- Estado en memoria: un reinicio cierra las salas.
- El job `e2e` de CI clona `main` del webrelay y usa su `E2E_BRIDGE_URL`
  (ya integrado); un cambio incompatible allí lo pondría en rojo.

## Plan de ejecución

Ver `../../tasks/bridge-mvp/TASKS.md`.

## Plan de validación

- `make ci`, `make e2e`.
- Manual: `just run` + `just dev` (webrelay) + `just mediamtx` + `just demo --live`.
- Manual con YouTube/Twitch desde la consola.
