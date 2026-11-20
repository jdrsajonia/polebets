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

Plan: redes por zona de confianza según la tabla; redes de datos y de backend con
`internal: true`; el Ingestor es el único **servicio de backend** que conserva
salida a Internet.

> **Matiz importante.** Una red con puertos publicados al host **no puede** ser
> `internal: true` (Docker lo prohíbe). Por eso solo `borde` (Gateway, Front,
> 80/443) tiene salida a Internet de forma inevitable. Eso es aceptable: son los
> nodos de **borde**, los que por diseño reciben tráfico de afuera. Admin dejó de
> ser nodo de borde en el escenario 2 (T3-03): ya no publica el 8003 y solo se
> alcanza por el Gateway. Lo que la segmentación garantiza es que **las bases de datos y los
> servicios de backend internos no salen**, y que el único backend con salida es el
> Ingestor (OpenF1).

Evidencia completa del *antes*: [`antes/evidencia-antes.md`](antes/evidencia-antes.md).

---

## 3. Táctica y patrón aplicados (la contramedida)

**Patrón:** Network Segmentation. **Tácticas (Bass §11.2):** *Separate entities*
(separar componentes en redes distintas limita el alcance de un ataque) y
*Limit exposure* (reducir los servicios alcanzables desde un mismo punto limita el
daño si ese punto se compromete).

En Docker, cada red está aislada por defecto: un contenedor solo ve a los que
comparten red con él, así que la **membresía de red funciona como regla de
filtrado**. Se reemplazó la red `interna` plana (10 servicios juntos) por redes
por zona de confianza, una por cada relación de la tabla de arriba.

### Implementación (`docker-compose.yml`)

| Red | Miembros | `internal` |
|---|---|---|
| `borde` | gateway, front, cli (cliente externo) | no (entrada web) |
| `gw_backend` | gateway, usuarios, orquestador, motor | sí |
| `orq_backend` | orquestador, usuarios, pagos, motor | sí |
| `motor_pagos` | motor, pagos | sí |
| `gw_admin` | gateway, admin | sí |
| `admin_usuarios` | admin, usuarios | sí |
| `broker` | admin, pagos-consumer, ingestor, rabbitmq | sí |
| `datos_usuarios` | usuarios, postgres-usuarios | sí |
| `datos_pagos` | pagos, pagos-consumer, postgres-pagos | sí |
| `datos_mongo` | motor, ingestor, mongo | sí |
| `datos_redis` | motor, redis | sí |
| `ingestor_salida` | ingestor | **no** (único backend con salida a Internet) |

> Actualizado con el escenario 2 (T3-03): `admin_entrada` (Admin publicado en
> 8003) se reemplazó por `gw_admin`; Admin solo se alcanza por el Gateway. La
> evidencia de esta carpeta es anterior a ese cambio.

Cambios clave: se eliminó la red `interna`; las redes de datos y las internas de
backend llevan `internal: true` (sin salida a Internet); el Ingestor obtiene una
red propia `ingestor_salida` no interna para descargar OpenF1. RabbitMQ pasó de la
red `interna` a `broker`, compartida solo con quienes publican/consumen.

Solo la red `borde` **no** es `internal`, porque publica puertos al host
(80/443): Docker no permite `internal: true` en una red con puertos publicados.
Por eso Gateway y Front —los nodos de borde— sí tienen salida a Internet, lo cual
es aceptable para componentes que ya reciben tráfico de afuera. Admin ya no: desde
el escenario 2 sus redes (`gw_admin`, `admin_usuarios`, `broker`) son todas
internas.

---

## 4. La misma prueba, con la contramedida funcionando

Evidencia completa del *después*: [`despues/evidencia-despues.md`](despues/evidencia-despues.md).

Resumen (mismo plan de pruebas que el *antes*):

**Violaciones cerradas:**

| # | Prueba | Antes | Después |
|---|---|---|---|
| A1 | Gateway → RabbitMQ `:15672` | Sirve la consola | `bad address` |
| A2 | Front → Pagos | `status: ok` | `bad address` |
| A3 | Front → Orquestador | `estado: ok` | `bad address` |
| A4 | Front → Usuarios | `status: ok` | `bad address` |
| A5 | Postgres → Internet | Resuelve `github.com` | `SERVFAIL` |
| A6 | Front → suplantación `/billetera` | `401`→`500` (aceptada) | `bad address` |

**Enlaces legítimos conservados:**

| # | Prueba | Antes | Después |
|---|---|---|---|
| B1 | Gateway → Motor (enlace del WebSocket) | `status: ok` | `status: ok` |
| B2 | Gateway → `/internal/auth` | `401` (responde) | `401` (responde) |
| B3 | Ingestor → Internet | descarga OpenF1 | descarga OpenF1 |
| — | App completa (login/compra/apuesta/WS) | funciona | funciona |

**Nota sobre B1 y B2.** El Gateway debe alcanzar al Motor (por ese enlace de red
le reenvía el WebSocket `/api/ws`) y a Usuarios (`auth_request`). No eran
violaciones a cerrar; se verifican como enlaces legítimos.

**Alcance:** la segmentación mitiga el **movimiento lateral** (reduce quién llega a
cada servicio). No cierra la suplantación *desde un componente que sí debe llegar*
(p. ej. el Gateway hacia el Orquestador): esa raíz la aborda el **escenario 4**
(autenticación entre servicios). Los dos escenarios se complementan.

---

## 5. Qué cambia en las vistas

- **Vista C&C:** desaparece la red `interna` única; aparecen conectores de red por
  zona de confianza (`gw_backend`, `orq_backend`, `motor_pagos`, `admin_usuarios`,
  `broker`, etc.). RabbitMQ ya no está en una red compartida con Gateway/Front.
- **Vista de despliegue:** cada contenedor se despliega solo en las redes que
  necesita; las redes de datos y de backend son `internal` (sin salida a
  Internet); el Ingestor es el único backend con un enlace a Internet
  (`ingestor_salida`) para OpenF1. Los nodos de borde (Gateway y Front; Admin
  dejó de serlo en el escenario 2) tienen salida porque su red publica puertos al
  host y no puede ser `internal`. (Aporta a T3-09 y T3-10.)
