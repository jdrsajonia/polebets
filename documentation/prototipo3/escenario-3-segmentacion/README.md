# Escenario 3 — Network Segmentation (T3-02)

**Dueño:** Tomás
**Patrón:** Network Segmentation
**Tácticas (Bass, §11.2):** *Separate entities*, *Limit exposure* (categoría **resistir**)
**Atributos de calidad:** Confidencialidad, integridad
**Rama de trabajo:** `p3/segmentacion`

> Estructura exigida por el roadmap, por separado:
> 1. El problema / vulnerabilidad, justificado sobre la vista C&C y la de despliegue.
> 2. Evidencia de la vulnerabilidad (comandos, salidas).
> 3. Táctica y patrón aplicados, y su implementación. *(pendiente: "después")*
> 4. La misma prueba repetida, con la contramedida funcionando. *(pendiente: "después")*
> 5. Qué cambia en las vistas C&C y de despliegue. *(pendiente: "después")*

---

## 1. El problema (antes)

Cada base de datos ya está en su propia red (`datos_usuarios`, `datos_pagos`,
`datos_mongo`, `datos_redis`), pero la **red `interna` es plana**: los 10
contenedores del stack lógico comparten una sola red y se ven todos entre sí.

### Sobre la vista C&C

Varios conectores del sistema asumen una restricción que la red **no garantiza**:

- **Orquestador y Motor confían en la cabecera `X-User-Id`** sin verificar nada
  más (`services/orquestador/internal/sesion/sesion.go`,
  `services/motor/internal/auth/middleware.go`). Es seguro *solo* porque el
  Gateway borra la cabecera que manda el cliente. Pero cualquier contenedor de la
  red interna puede llamar directo a `orquestador:8081` con el `X-User-Id` de otro
  jugador y actuar como él (apostar, comprar, ver su información).
- **`GET /internal/auth` de Usuarios no pide token**; su seguridad depende de que
  "solo el Gateway la alcanza".
- **Las rutas `/internal/*` de Pagos y Usuarios** están protegidas solo por un
  token compartido.
- **La consola de RabbitMQ (`:15672`)** y sus colas quedan expuestas: desde la red
  interna se pueden publicar órdenes en las colas que usa Admin (p. ej. iniciar una
  simulación).

### Sobre la vista de despliegue

- **Gateway, Front y Admin** reciben tráfico de afuera (son los nodos más
  expuestos) y, a la vez, están los tres en la red `interna`. Si uno se
  compromete, el atacante queda **dentro** de la red interna y alcanza todo lo
  anterior: el daño no queda contenido (falla *Limit exposure*).
- **Ninguna red tiene `internal: true`**, así que todos los contenedores —incluidas
  las bases de datos— pueden **salir a Internet** (exfiltración de datos o descarga
  de herramientas desde cualquier contenedor).

### Riesgo

El patrón que se mitiga es el **movimiento lateral**: la postura de seguridad de
media docena de servicios se apoya en "solo el Gateway me llama", y la topología de
red actual no lo hace cumplir.

---

## 2. Evidencia de la vulnerabilidad (antes)

Comandos exactos en [`antes/comandos-antes.sh`](antes/comandos-antes.sh); salida
completa en [`antes/salida-antes.txt`](antes/salida-antes.txt). Reproducir con:

```bash
bash antes/comandos-antes.sh 2>&1 | tee antes/salida-antes.txt
```

Topología de partida: **los 10 servicios lógicos comparten la red `interna`**, y
Gateway / Front / Admin están además en `borde`. Alcance indebido observado:

| # | Origen → Destino | Resultado observado | Debería |
|---|---|---|---|
| 1 | Gateway → RabbitMQ `:15672` | Sirve la consola HTML de RabbitMQ Management | No alcanzar |
| 2 | Front → Pagos `:8000/health` | `{"service":"pagos","status":"ok",...}` | No alcanzar |
| 3 | Front → Orquestador `:8081/health` | `{"estado":"ok"}` | No alcanzar |
| 4 | Gateway → Motor `:8080/health` | `{"service":"motor","status":"ok",...}` | No alcanzar |
| 5 | Front → Usuarios `:8001/health` | `{"service":"usuarios","status":"ok",...}` | No alcanzar |
| 6 | Gateway → Usuarios `/internal/auth` | `HTTP/1.1 401` (responde → **la alcanza**) | No alcanzar |
| 7 | Postgres usuarios → `github.com` | Resuelve `140.82.113.3` (salida a Internet) | Sin salida |

En la prueba 6, el `401` no es la defensa: demuestra que **hay conectividad de red**
(el servicio contesta). La ruta `/internal/auth` no exige token; su única barrera
es "nadie más llega", y las pruebas muestran que eso no se cumple.

---

## Contramedida (quién habla con quién) — para la fase "después"

Redes por zona de confianza según las variables de entorno del compose:

| Componente | Necesita llegar a |
|---|---|
| Gateway | Usuarios, Orquestador, Motor, Front |
| Front (SSR) | Gateway |
| Admin | Usuarios, RabbitMQ |
| Orquestador | Usuarios, Pagos, Motor |
| Motor | Pagos, Mongo, Redis |
| Pagos | PG Pagos |
| Consumidor de Pagos | PG Pagos, RabbitMQ |
| Usuarios | PG Usuarios |
| Ingestor | Mongo, RabbitMQ, OpenF1 (Internet) |

Plan: redes por zona de confianza según la tabla; redes de datos con
`internal: true`; **solo el Ingestor** conserva salida a Internet. La evidencia
"después" repetirá las 7 pruebas; se espera que 1–6 fallen (el nombre ni siquiera
resuelve) y que 7 falle para las bases pero siga funcionando para el Ingestor.
