# PoleBet — Prototipo 2 (ArquiSoft)

Plataforma de apuestas en vivo sobre carreras de F1 con **tokens simulados**
(sin dinero real). Este repositorio contiene el **Prototipo 2**: una
arquitectura distribuida de microservicios con API Gateway, Orquestador,
mensajería asíncrona (RabbitMQ), cuatro bases de datos y cinco lenguajes.

> **Estado de la base (T2-04).** Este commit deja montada la **infraestructura
> completa** del prototipo 2: `docker compose up` levanta los 14 contenedores,
> con sus redes, puertos y healthchecks ya definidos. Los servicios de lógica
> que todavía no tienen su implementación final corren como **stub** (solo
> responden `GET /health`) para que cada dueño construya el suyo encima sin
> tocar la topología. Ver la tabla «Estado por servicio» más abajo.

## Arquitectura (Vista C&C)

Los jugadores entran **solo** por el API Gateway; el administrador, **solo** por
Admin. Ningún servicio comparte base de datos con otro: todo dato ajeno se pide
por contrato (REST, WebSocket o AMQP).

| Componente        | Tipo          | Lenguaje / Stack     | Datos               |
| ----------------- | ------------- | -------------------- | ------------------- |
| **Gateway**       | Presentación/Comunicación | Nginx    | —                   |
| **Front**         | Presentación  | React + Vite (Nginx) | —                   |
| **Orquestador**   | Orquestación  | Go                   | —                   |
| **Usuarios**      | Lógica        | Python / FastAPI     | PostgreSQL Usuarios |
| **Pagos** (+ consumidor AMQP) | Lógica | Python / FastAPI  | PostgreSQL Pagos    |
| **Motor**         | Lógica        | Go                   | lee MongoDB, escribe Redis |
| **Ingestor**      | Lógica        | Go                   | escribe MongoDB     |
| **Admin**         | Lógica        | Python / FastAPI     | —                   |
| **Broker**        | Asíncrono     | RabbitMQ             | —                   |

Las dos únicas puertas al host son **Gateway (80)** y **Admin (8003)**; todos
los demás puertos existen solo dentro de las redes de Docker (RNF-17).

```
services/
├── gateway/      # Nginx: única entrada web (auth_request + enrutamiento)   [stub → T2-01]
├── front/        # React + Vite servido por Nginx (SSR en HU2-03)
├── orquestador/  # Go: coordina registro, SSR, inscripción y apuesta         [T2-02]
├── usuarios/     # FastAPI: cuentas, JWT, auth_request                        [stub → HU2-01/02/03]
├── pagos/        # FastAPI: billeteras, eventos, mercados, apuestas, ledger
├── motor/        # Go: mercados, apuestas, liquidación, difusión WebSocket
├── ingestor/     # Go: descarga OpenF1 y reproduce la carrera en MongoDB      [stub → HU2-16/19]
└── admin/        # FastAPI: atiende la CLI, publica en el Broker              [stub → HU2-18/19]
```

## Requisitos

- [Docker](https://docs.docker.com/get-docker/) con Docker Compose v2
  (`docker compose`). No hace falta instalar Go, Python ni Node en la máquina.

## Levantar el sistema

Un solo comando desde la raíz del repositorio:

```bash
cp .env.example .env     # solo la primera vez
docker compose up --build
```

Para desarrollo local los valores por defecto de `.env.example` ya funcionan.
Antes de un entorno compartido, cambia `JWT_SECRET`, `INTERNAL_API_TOKEN`
(mínimo 32 caracteres) y `ADMIN_PASSWORD`.

Cuando el stack esté arriba, las dos entradas públicas:

- **Gateway (jugadores):** http://localhost/health
- **Admin (CLI del administrador):** http://localhost:8003/health

Comprobar que los 14 contenedores están sanos:

```bash
docker compose ps
```

Detener y limpiar:

```bash
docker compose down          # detiene los contenedores
docker compose down -v       # además borra los volúmenes de datos (BD)
```

> Si cambias el esquema de alguna base o los scripts de `db/`, recrea los
> volúmenes con `docker compose down -v` antes de volver a levantar.

## Estado por servicio

| Servicio          | Estado | Dueño / Historia         |
| ----------------- | ------ | ------------------------ |
| `gateway`         | **stub** (responde `/health`) | T2-01 · Tomás |
| `front`           | real (del P1; SSR pendiente)  | HU2-03 · Misael |
| `orquestador`     | real (base del servicio)      | T2-02 · Daniel |
| `usuarios`        | **stub** (tabla `users` lista, T2-03) | HU2-01/02/03 · Misael |
| `pagos`           | real (tablas del P2, T2-03)   | HU2-04/07… · Juan Diego |
| `pagos-consumer`  | **stub** (placeholder AMQP)   | HU2-18 · Iván |
| `motor`           | real                          | HU2-08/09… · Daniel/Michael |
| `ingestor`        | **stub**                      | HU2-16/19 · Iván |
| `admin`           | **stub**                      | HU2-18/19 · Iván |
| Infra (2×PostgreSQL, MongoDB, Redis, RabbitMQ) | real (imágenes oficiales) | — |

Cada stub tiene su propio `README.md` dentro de `services/<nombre>/` con la
lista de lo que falta implementar.

## Redes y propiedad de los datos (RNF-18)

Tres planos de red: **borde** (gateway, front, admin), **interna** (servicios de
lógica y Broker) y **datos**. Cada base vive en su propia red de datos y la
comparte **solo** con su servicio dueño:

| Red de datos      | Contenedores                                   |
| ----------------- | ---------------------------------------------- |
| `datos_usuarios`  | postgres-usuarios, usuarios                    |
| `datos_pagos`     | postgres-pagos, pagos, pagos-consumer          |
| `datos_mongo`     | mongo, ingestor (escribe), motor (lee)         |
| `datos_redis`     | redis, motor                                   |

Así el Motor no puede siquiera resolver el DNS de la base de Pagos, y viceversa.

## Configuración

Todo se configura en `.env` (copiado de `.env.example`). Variables clave:

| Variable                                   | Descripción                                    |
| ------------------------------------------ | ---------------------------------------------- |
| `USUARIOS_DB_*`                            | Credenciales de la PostgreSQL de Usuarios      |
| `PAGOS_DB_*`                               | Credenciales de la PostgreSQL de Pagos         |
| `MONGO_*`                                  | Credenciales de MongoDB                        |
| `RABBITMQ_*`                               | Credenciales del Broker                        |
| `JWT_SECRET`                               | Secreto JWT (lo emite Usuarios, lo validan los servicios) |
| `INTERNAL_API_TOKEN`                       | Token de servicio para `/internal/*` (mín. 32 caracteres) |
| `COP_PER_TOKEN`                            | Tasa fija COP → token (RF-05)                  |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | Credenciales del admin inicial            |
| `BETTING_WINDOW_SECONDS` / `OBSERVATION_WINDOW_SECONDS` | Ciclo del mercado (RF-10/13, RNF-19) |

## Datos del prototipo 1 (T2-03)

Usuarios y Pagos crean sus tablas al arrancar, vacías. Para traer las cuentas y
los saldos del P1, con el stack del P2 arriba:

```bash
scripts/migrar-p1.sh   # P1_DB_NAME=<POSTGRES_DB del .env del P1> si no era apuestas_db
```

Copia `users` (sin `balance` ni `points`) a PostgreSQL Usuarios y cada saldo a
`wallets` en PostgreSQL Pagos, y falla si los totales no coinciden. El historial
de apuestas del P1 no se migra. Detalle en el encabezado del script.

## Documentación

- Modelo de datos del prototipo 2: [`documentation/modelo-datos-p2.pdf`](documentation/modelo-datos-p2.pdf)
- Contrato del Orquestador: [`documentation/orquestador-api.md`](documentation/orquestador-api.md)
- Historias, requisitos y vista C&C del prototipo 2: PDFs en la raíz del repo.
- Cada servicio documenta su parte en `services/<nombre>/README.md`.

> **Ingesta de OpenF1 y simulación de carreras:** llegan con el Ingestor real
> (HU2-16) y el flujo de iniciar simulación desde la CLI (HU2-19). Mientras
> tanto, HU2-07 se prueba con una simulación programada escrita a mano en
> MongoDB. El detalle del P1 queda como referencia en
> [`documentation/hu-04-ingesta-openf1.md`](documentation/hu-04-ingesta-openf1.md)
> y [`documentation/hu-07-liquidacion-apuestas.md`](documentation/hu-07-liquidacion-apuestas.md).
