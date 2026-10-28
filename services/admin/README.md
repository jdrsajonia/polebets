# Admin (stub base — T2-04)

Esqueleto del servicio **Admin** (Python / FastAPI). Dueño: **Iván** (HU2-18, HU2-19, HU2-23).

Hoy solo responde `GET /health`. Es el **único servicio de lógica publicado al host**
(puerto **8003**): la CLI del administrador (Rust, William) habla solo con él.

## Qué implementar aquí

- Validar el login con rol `admin` contra Usuarios (RF-21; rol jugador se rechaza).
- Publicar en el **Broker (RabbitMQ)**:
  - Órdenes de simulación para el Ingestor (HU2-19, mensajería de comando).
  - Consultas del administrador para Pagos (HU2-18, petición-respuesta con `correlation_id`).
- Devolver las respuestas a la CLI; si una consulta no llega en [10 s], avisar que tardó demasiado.

## Red y puerto

- Vive en las redes **borde** (lo alcanza la CLI desde el host) e **interna** (habla con el Broker y Usuarios).
- Puerto **8003**, publicado al host. Es la segunda y última puerta de entrada junto al Gateway.
