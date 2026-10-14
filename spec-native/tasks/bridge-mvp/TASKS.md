# TASKS — bridge-mvp

```toml
artifact_type = "task_file"
initiative    = "bridge-mvp"
spec_id       = "SPEC-0001"
owner         = "rafex"
state         = "in_progress"
```

## Tareas

### TASK-0001 - Dominio: sala, invitaciones, perfil, mensajes v2 y plan de composición

```toml
id             = "TASK-0001"
title          = "Dominio: sala, invitaciones, perfil, mensajes v2 y plan de composición"
state          = "done"
priority       = "p0"
owner          = "rafex"
labels         = ["domain"]
dependencies   = []
expected_files = ["source/backend/go/bridge/internal/domain/*"]
close_criteria = "Validación de mensajes v2, aforo/escena/destinos, HMAC, layouts con tests"
validation     = ["make test-unit"]
completion_evidence = ["internal/domain: cobertura 98.6 %"]
```

### TASK-0002 - Aplicación: Hub, Session y Broadcaster

```toml
id             = "TASK-0002"
title          = "Aplicación: Hub, Session y Broadcaster"
state          = "done"
priority       = "p0"
owner          = "rafex"
labels         = ["application"]
dependencies   = ["TASK-0001"]
expected_files = ["source/backend/go/bridge/internal/application/*", "source/backend/go/bridge/internal/ports/*"]
close_criteria = "Peek/join/resume/kick/cierre y bucle de emisión probados con dobles"
validation     = ["make test-unit"]
completion_evidence = ["internal/application: cobertura 92.9 % con -race"]
```

### TASK-0003 - SFU con Pion

```toml
id             = "TASK-0003"
title          = "SFU con Pion"
state          = "done"
priority       = "p0"
owner          = "rafex"
labels         = ["webrtc", "infrastructure"]
dependencies   = ["TASK-0002"]
expected_files = ["source/backend/go/bridge/internal/infrastructure/sfu/*"]
close_criteria = "Dos clientes Pion intercambian RTP a través del SFU; taps y PLI"
validation     = ["make test-integration"]
completion_evidence = ["sfu_test: publicador → suscriptor y tap verificados (83.4 %)"]
```

### TASK-0004 - HTTP: señalización WebSocket, API REST y consola

```toml
id             = "TASK-0004"
title          = "HTTP: señalización WebSocket, API REST y consola"
state          = "done"
priority       = "p0"
owner          = "rafex"
labels         = ["api", "console"]
dependencies   = ["TASK-0002"]
expected_files = ["source/backend/go/bridge/internal/infrastructure/httpapi/*"]
close_criteria = "Subprotocolo/origen exigidos, API autenticada, consola embebida con CSP"
validation     = ["make test-integration", "prueba manual de la consola"]
completion_evidence = ["httpapi: 86.1 %", "navegador: consola con sala, participantes, destinos, layout e invitación"]
```

### TASK-0005 - Compositor FFmpeg con empalme sin cortes

```toml
id             = "TASK-0005"
title          = "Compositor FFmpeg con empalme sin cortes"
state          = "done"
priority       = "p0"
owner          = "rafex"
labels         = ["ffmpeg", "composition"]
dependencies   = ["TASK-0003"]
expected_files = ["source/backend/go/bridge/internal/infrastructure/ffmpeg/*"]
close_criteria = "Emisión continua a través de cambios de layout; SPS estable; publicación tee"
validation     = ["make test-integration", "prueba con MediaMTX"]
completion_evidence = ["MediaMTX: 45 s con entradas, pantalla, cámara apagada y salidas; decodifica sin errores ni reconexión", "compositor_integration_test: dos ejecuciones → un flujo"]
```

### TASK-0006 - Tooling ether-rules, contenedores y CI

```toml
id             = "TASK-0006"
title          = "Tooling ether-rules, contenedores y CI"
state          = "done"
priority       = "p1"
owner          = "rafex"
labels         = ["tooling", "container", "ci"]
dependencies   = ["TASK-0001"]
expected_files = ["Makefile", "Justfile", "helpers/*", "containers/*", ".github/workflows/ci.yml"]
close_criteria = "make ci en contenedor; imagen del puente sana; MediaMTX y demo por just"
validation     = ["make ci", "just up --dev"]
completion_evidence = ["make ci: cobertura 88.8 % en contenedor", "just up --dev + just mediamtx + just demo: emisión con WebRTC por el puerto UDP publicado"]
```

### TASK-0007 - E2E del webrelay contra el puente

```toml
id             = "TASK-0007"
title          = "E2E del webrelay contra el puente"
state          = "done"
priority       = "p0"
owner          = "rafex"
labels         = ["testing", "e2e"]
dependencies   = ["TASK-0004", "TASK-0003"]
expected_files = ["helpers/shell/go.sh"]
close_criteria = "Los 5 escenarios del webrelay pasan contra el puente"
validation     = ["make e2e"]
completion_evidence = ["make e2e: 5 passed"]
```

### TASK-0008 - Documentación y contexto SpecNative

```toml
id             = "TASK-0008"
title          = "Documentación y contexto SpecNative"
state          = "done"
priority       = "p1"
owner          = "rafex"
labels         = ["docs"]
dependencies   = ["TASK-0005"]
expected_files = ["docs/*", "spec-native/*", "README.md"]
close_criteria = "Guías, API, arquitectura, composición, manuales; decisiones registradas"
validation     = ["make docs"]
completion_evidence = ["docs/ y spec-native/ completos"]
```

### TASK-0009 - Prueba con un destino real

```toml
id             = "TASK-0009"
title          = "Prueba con un destino real"
state          = "todo"
priority       = "p1"
owner          = "rafex"
labels         = ["manual", "rtmp"]
dependencies   = ["TASK-0005"]
expected_files = []
close_criteria = "La sala se ve en YouTube o Twitch desde la consola"
validation     = ["prueba manual"]
completion_evidence = []
```

TASK-0009 necesita una cuenta con emisión en directo habilitada (manual).
