# Escenario 3 — Network Segmentation · Evidencia del *antes*

**Rama:** `p3/segmentacion`

**Estado capturado:** red `interna` plana (todavía sin segmentar)

Todos los comandos son de solo lectura (no modifican el sistema)

## 0. Topología de partida

### 0a. Redes del proyecto

```bash 
docker network ls | grep -i arqui
```

![Redes del proyecto](img/00a-redes.png)

### 0b. Miembros de la red `interna`

**Los 10 servicios lógicos comparten una sola red**: de ahí que se vean todos
entre sí

```bash
docker network inspect arquisoft-prototipo2_interna \
  --format '{{range .Containers}}{{.Name}}{{"\n"}}{{end}}' | sort
```

![Miembros de la red interna](img/00b-interna.png)

### 0c. Redes de los servicios de borde

Gateway, Front y Admin (los que reciben tráfico de afuera) están a la vez en
`borde` y en `interna`

```bash
for c in arqui_gateway arqui_front arqui_admin; do
  echo "--- $c ---"
  docker inspect "$c" --format '{{range $n,$v := .NetworkSettings.Networks}}{{$n}}{{"\n"}}{{end}}'
done
```

![Redes de los servicios de borde](img/00c-borde.png)

---

## Violaciones de alcance (movimiento lateral)

Cada prueba: un origen de borde alcanza un destino que no le corresponde
según la tabla "quién habla con quién" del roadmap

### 1. Gateway → consola de RabbitMQ (`:15672`)

El Gateway no debería ver RabbitMQ; aquí le sirve la consola de administración

```bash
docker exec arqui_gateway wget -qO- --timeout=5 http://rabbitmq:15672 | head -c 300
```

**Resultado:** ⚠️ responde con el HTML de *RabbitMQ Management*.

![Gateway alcanza RabbitMQ](img/01-gateway-rabbitmq.png)

### 2. Front → Pagos (`:8000/health`)

```bash
docker exec arqui_front wget -qO- --timeout=5 http://pagos:8000/health
```

**Resultado:** ⚠️ el Front alcanza Pagos (no debería hablar con él)

![Front alcanza Pagos](img/02-front-pagos.png)

### 3. Front → Orquestador (`:8081/health`)

```bash
docker exec arqui_front wget -qO- --timeout=5 http://orquestador:8081/health
```

**Resultado:** ⚠️ el Front alcanza el Orquestador (solo debería hablar con el Gateway)

![Front alcanza Orquestador](img/03-front-orquestador.png)

### 4. Gateway → Motor (`:8080/health`)

```bash
docker exec arqui_gateway wget -qO- --timeout=5 http://motor:8080/health
```

**Resultado:** ⚠️ el Gateway alcanza el Motor directo

![Gateway alcanza Motor](img/04-gateway-motor.png)

### 5. Front → Usuarios (`:8001/health`)

```bash
docker exec arqui_front wget -qO- --timeout=5 http://usuarios:8001/health
```

**Resultado:** ⚠️ el Front alcanza Usuarios

![Front alcanza Usuarios](img/05-front-usuarios.png)

### 6. Gateway → `/internal/auth` de Usuarios

`GET /internal/auth` **no pide token**: su única barrera es "solo el Gateway me
llama". El `401` no es la defensa — prueba que hay conectividad de red (el
servicio responde)

```bash
docker exec arqui_gateway wget -S -qO- --timeout=5 http://usuarios:8001/internal/auth 2>&1 | head -5
```

**Resultado:** ⚠️ el servicio contesta → el Gateway alcanza la ruta interna

![Gateway alcanza /internal/auth](img/06-gateway-internal-auth.png)

### 7. Base de datos → Internet

Ninguna red tiene `internal: true`, así que hasta una base de datos puede salir a
Internet (riesgo de exfiltración / descarga de herramientas)

```bash
docker exec arqui_pg_usuarios sh -c 'getent hosts github.com || nslookup github.com'
```

**Resultado:** ⚠️ Postgres de usuarios resuelve y sale a Internet

![Postgres sale a Internet](img/07-pg-internet.png)

### 8. Front → `/billetera` con `X-User-Id` a mano (suplantación)

El Front no debería llegar al Orquestador. Además, escribiendo `X-User-Id` a mano
actúa como otro jugador. El contraste lo demuestra: sin el header responde
`401`; con el header responde `500` (ya no 401), es decir el Orquestador
aceptó la identidad falsificada y entró al handler (el 500 es porque ese
jugador no existe en Pagos)

> La raíz —confiar en `X-User-Id` sin firma— la cierra el escenario 4
> (autenticación entre servicios). La segmentación corta este vector cuando el
> origen deja de ver al destino, como aquí el Front. La suplantación *desde el
> Gateway* queda fuera del alcance de este escenario, porque el Gateway debe seguir
> hablando con el Orquestador.

```bash
# sin header: no autenticado
docker exec arqui_front wget -S -qO- --timeout=5 http://orquestador:8081/billetera 2>&1 | grep "HTTP/"
# con header a mano: suplantación
docker exec arqui_front wget -S -qO- --timeout=5 \
  --header="X-User-Id: 00000000-0000-0000-0000-000000000001" \
  --header="X-User-Rol: player" \
  http://orquestador:8081/billetera 2>&1 | grep "HTTP/"
```

**Resultado:** ⚠️ `401` sin header → `500` con header: la identidad falsa fue aceptada

![Front suplanta jugador en Orquestador](img/08-front-suplantacion.png)

### 9. Ingestor → Internet (debe seguir funcionando después)

El Ingestor es el único que conserva salida a Internet tras la segmentación
(descarga el histórico de OpenF1). No es una vulnerabilidad: es el caso que debe
mantenerse para no romper la app

```bash
docker exec arqui_ingestor sh -c 'getent hosts api.openf1.org || nslookup api.openf1.org'
```

**Resultado:** ✅ el Ingestor resuelve `api.openf1.org` y sale a Internet

![Ingestor sale a Internet](img/09-ingestor-internet.png)

---

## Resumen

| # | Origen → Destino | Resultado | Debería |
|---|---|---|---|
| 1 | Gateway → RabbitMQ `:15672` | Sirve la consola HTML | No alcanzar |
| 2 | Front → Pagos `:8000` | `status: ok` | No alcanzar |
| 3 | Front → Orquestador `:8081` | `estado: ok` | No alcanzar |
| 4 | Gateway → Motor `:8080` | `status: ok` | *(enlace legítimo)* |
| 5 | Front → Usuarios `:8001` | `status: ok` | No alcanzar |
| 6 | Gateway → `/internal/auth` | `401` (responde → alcanza) | *(enlace legítimo)* |
| 7 | Postgres usuarios → `github.com` | Resuelve `140.82.114.4` | Sin salida a Internet |
| 8 | Front → `/billetera` con `X-User-Id` a mano | `401` → `500`: identidad falsa aceptada | No alcanzar |
| 9 | Ingestor → `api.openf1.org` | Resuelve y sale a Internet | Seguir (único con salida) |

> **Matiz sobre las pruebas 4 y 6.** Según la tabla de confianza, el Gateway **sí**
> debe hablar con Motor (WebSocket) y con Usuarios (`auth_request`), así que no son
> enlaces a cortar: tras segmentar **seguirán funcionando** (sección A del
> *después*). Se incluyen para documentar el estado de la red plana.

Las violaciones a cerrar son 1, 2, 3, 5, 7 y 8; la 9 es el caso que debe seguir
funcionando. La fase *después* repite exactamente estas pruebas con la
segmentación aplicada.
