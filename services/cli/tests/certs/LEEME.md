# Certificados de prueba (solo para `cargo test`)

CA y certificado de servidor para `localhost` que usan las pruebas de HTTPS de
`src/api.rs`. No sirven para el Gateway ni para nada fuera de las pruebas: los
del Gateway los genera cada quien con `scripts/generar-certs.sh`.

- `ca.crt`: CA de pruebas («PoleBet CA de pruebas»). Su llave se borró al
  generarla, así que no puede firmar nada más.
- `servidor.crt` / `servidor.key`: certificado firmado por esa CA. Su SAN es solo
  `DNS:localhost`, para probar que `https://127.0.0.1` se rechaza por nombre.

Se generaron con la receta del Lab 5 (EC P-256, `CA:FALSE`, `serverAuth`), con
validez de 10 años para que las pruebas no fallen por vencimiento (hasta 2036).
La llave está en el repo a propósito: es pública y solo la usa el servidor de
prueba.
