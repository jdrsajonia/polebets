// api.js no exporta tipos (es JavaScript); en las pruebas se usan dobles de
// fetch sin tiparlos como Response completo.
/* eslint-disable @typescript-eslint/no-explicit-any */
import { describe, expect, it } from "vitest";
import { apostar, formatoReloj, puntosPotenciales, segundosRestantes, validarMonto } from "./api.js";

describe("logica de HU-06 (apuestas)", () => {
  it("puntos potenciales: 10 + monto x 0,5", () => {
    expect(puntosPotenciales(100)).toBe(60);
    expect(puntosPotenciales(1)).toBe(10.5);
  });

  it("cuenta regresiva", () => {
    const ahora = Date.parse("2026-10-06T12:00:00Z");
    expect(segundosRestantes("2026-10-06T12:00:30Z", ahora)).toBe(30);
    expect(segundosRestantes("2026-10-06T11:59:00Z", ahora)).toBe(0); // nunca negativa
    expect(segundosRestantes("basura", ahora)).toBe(0);
    expect(formatoReloj(75)).toBe("1:15");
    expect(formatoReloj(5)).toBe("0:05");
  });

  it("validar monto", () => {
    expect(validarMonto("").ok).toBe(false);
    expect(validarMonto("").error).toBe(""); // vacio: sin mensaje de error
    expect(validarMonto("0").ok).toBe(false);
    expect(validarMonto("-5").ok).toBe(false);
    expect(validarMonto("12.5").ok).toBe(false);
    expect(validarMonto("abc").ok).toBe(false);
    expect(validarMonto(" 40 ")).toEqual({ ok: true, monto: 40, error: "" });
    expect(validarMonto("500", 100 as any).error).toBe("Saldo insuficiente");
    expect(validarMonto("100", 100 as any).ok).toBe(true);
  });

  it("apostar arma la peticion que espera el motor", async () => {
    let visto: any;
    const fetchFn = async (url: any, init: any) => {
      visto = { url, init };
      return { ok: true, status: 201, json: async () => ({ bet_id: "x" }) } as any;
    };
    const r = await apostar({ mercadoId: "m1", opcion: "VER", monto: 100, clave: "k1", fetchFn, baseUrl: "http://motor" });
    expect(r.ok).toBe(true);
    expect(visto.url).toBe("http://motor/apuestas");
    expect(visto.init.method).toBe("POST");
    expect(visto.init.credentials).toBe("include"); // manda la cookie de sesion
    expect(visto.init.headers["Idempotency-Key"]).toBe("k1");
    expect(JSON.parse(visto.init.body)).toEqual({ market_id: "m1", option: "VER", amount: 100 });
  });

  it("apostar traduce los errores del motor", async () => {
    const resp = (status: number, cuerpo: unknown) => async () =>
      ({ ok: status < 300, status, json: async () => cuerpo }) as any;
    const base = { mercadoId: "m1", opcion: "VER", monto: 10, clave: "k" };

    let r = await apostar({ ...base, fetchFn: resp(422, { error: "Saldo insuficiente" }) });
    expect([r.ok, r.status, r.mensaje]).toEqual([false, 422, "Saldo insuficiente"]);

    r = await apostar({ ...base, fetchFn: resp(409, { error: "Mercado cerrado" }) });
    expect(r.mensaje).toBe("Mercado cerrado");

    r = await apostar({ ...base, fetchFn: resp(401, { error: "no autorizado" }) });
    expect(r.mensaje).toMatch(/sesion/i);

    r = await apostar({ ...base, fetchFn: async () => { throw new TypeError("fallo de red"); } });
    expect(r.status).toBe(0);
    expect(r.mensaje).toMatch(/conectar/i);

    r = await apostar({ ...base, fetchFn: async () => ({ ok: false, status: 500, json: async () => { throw new Error("no json"); } }) as any });
    expect(r.ok).toBe(false);
  });
});
