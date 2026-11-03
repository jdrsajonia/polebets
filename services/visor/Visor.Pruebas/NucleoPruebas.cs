// Pruebas de la lógica del visor (HU2-15): mensajes, proyección, reloj e interpolación.

using Visor.Nucleo;

namespace Visor.Pruebas;

public class MensajesPruebas
{
    [Fact]
    public void LeeLaPistaComoLaMandaElMotor()
    {
        var texto = """
            {"type":"pista","payload":{"simulacion_id":"b5a4","carrera_id":"9165","estado":"en_curso",
             "piloto_referencia":44,"vuelta_referencia":47,"puntos":[[1053,-305],[1083,-488],[900,-500]],
             "emitido_en":"2026-11-03T22:22:12.5Z"}}
            """;
        var pista = Assert.IsType<MensajeVisor.Pista>(MensajeVisor.Leer(texto)).Datos;
        Assert.Equal("9165", pista.CarreraId);
        Assert.Equal(44, pista.PilotoReferencia);
        Assert.Equal([1053.0, -305.0], pista.Puntos[0]);
    }

    [Fact]
    public void LeeLasCoordenadas()
    {
        var texto = """
            {"type":"coordenadas","payload":{"simulacion_id":"b5a4","carrera_id":"9165","t_carrera_ms":5788711,
             "muestras":[{"numero":1,"t":5787906,"x":-1797,"y":171}],"emitido_en":"2026-11-03T22:22:12.5Z"}}
            """;
        var c = Assert.IsType<MensajeVisor.Coordenadas>(MensajeVisor.Leer(texto)).Datos;
        Assert.Equal(5788711, c.TCarreraMs);
        Assert.Equal(new Muestra(1, 5787906, -1797, 171), c.Muestras[0]);
    }

    [Fact]
    public void LeeLasPosicionesConColorOpcional()
    {
        var texto = """
            {"type":"posiciones","payload":{"carrera_id":"9165","actualizado_en":"2026-11-03T22:22:12Z","vuelta":49,"total_vueltas":62,
             "pilotos":[{"posicion":1,"numero":55,"codigo":"SAI","nombre":"Carlos SAINZ","equipo":"Ferrari","color_equipo":"#E80020","diferencia":"Líder"},
                        {"posicion":2,"numero":4,"codigo":"NOR","nombre":"Lando NORRIS","equipo":"McLaren","diferencia":"—"}]}}
            """;
        var f = Assert.IsType<MensajeVisor.Posiciones>(MensajeVisor.Leer(texto)).Datos;
        Assert.Equal("#E80020", f.Pilotos[0].ColorEquipo);
        Assert.Null(f.Pilotos[1].ColorEquipo);
    }

    [Theory]
    [InlineData("""{"type":"mercado_dinamico","payload":{}}""")]
    [InlineData("no es json")]
    [InlineData("""{"payload":{}}""")]
    public void IgnoraLoQueNoEsDelVisor(string texto) => Assert.Null(MensajeVisor.Leer(texto));
}

public class ProyeccionPruebas
{
    [Fact]
    public void LaPistaQuedaDentroDelLienzoSinDeformarseYConLaYInvertida()
    {
        // Rectángulo de 2000 × 1000 en coordenadas de OpenF1.
        var p = new Proyeccion([[0, 0], [2000, 0], [2000, 1000], [0, 1000]]);
        Assert.Equal(Proyeccion.Margen, p.X(0), 3);
        Assert.Equal(Proyeccion.Ancho - Proyeccion.Margen, p.X(2000), 3);
        // Misma escala en x e y: 1000 de alto ocupan la mitad que 2000 de ancho.
        var escala = (p.X(2000) - p.X(0)) / 2000;
        Assert.Equal(1000 * escala, p.Y(0) - p.Y(1000), 3);
        // y = 1000 (arriba en OpenF1) queda arriba en el SVG (y menor).
        Assert.True(p.Y(1000) < p.Y(0));
        Assert.Equal(p.Alto / 2, (p.Y(0) + p.Y(1000)) / 2, 3);
    }

    [Fact]
    public void UnaPistaMuyAltaSeAcotaYQuedaCentrada()
    {
        var p = new Proyeccion([[0, 0], [100, 0], [100, 5000], [0, 5000]]);
        Assert.Equal(Proyeccion.AltoMaximo, p.Alto);
        Assert.Equal(Proyeccion.Ancho / 2, (p.X(0) + p.X(100)) / 2, 3);
        Assert.InRange(p.Y(0), 0, p.Alto);
        Assert.InRange(p.Y(5000), 0, p.Alto);
    }
}

public class InterpoladorPruebas
{
    [Fact]
    public void InterpolaEnLineaRectaEntreDosMuestras()
    {
        var i = new Interpolador();
        i.Agregar(new Muestra(1, 1000, 0, 0));
        i.Agregar(new Muestra(1, 1270, 270, -27));
        Assert.Equal((135.0, -13.5), i.PosicionEn(1, 1135));
    }

    [Fact]
    public void AntesDeLaPrimeraYDespuesDeLaUltimaSeQuedaQuieto()
    {
        var i = new Interpolador();
        i.Agregar(new Muestra(1, 1000, 5, 5));
        i.Agregar(new Muestra(1, 2000, 9, 9));
        Assert.Equal((5.0, 5.0), i.PosicionEn(1, 0));
        Assert.Equal((9.0, 9.0), i.PosicionEn(1, 9999));
        Assert.Null(i.PosicionEn(44, 1000));
    }

    [Fact]
    public void AceptaMuestrasDesordenadasEIgnoraLasRepetidas()
    {
        var i = new Interpolador();
        i.Agregar(new Muestra(1, 2000, 20, 0));
        i.Agregar(new Muestra(1, 1000, 10, 0));
        i.Agregar(new Muestra(1, 2000, 99, 99)); // repetida (un reenvío): se ignora
        Assert.Equal(2, i.Cantidad(1));
        Assert.Equal((15.0, 0.0), i.PosicionEn(1, 1500));
    }

    [Fact]
    public void RecortarDejaUnaMuestraAnteriorParaInterpolar()
    {
        var i = new Interpolador();
        for (var t = 0; t <= 10_000; t += 1000) i.Agregar(new Muestra(1, t, t, 0));
        i.Recortar(9500); // límite: 4500
        Assert.Equal(7, i.Cantidad(1)); // 4000..10000
        Assert.Equal((4500.0, 0.0), i.PosicionEn(1, 4500));
    }
}

public class RelojCarreraPruebas
{
    [Fact]
    public void DibujaConRetrasoYAvanzaConElRelojLocal()
    {
        var r = new RelojCarrera();
        Assert.Null(r.Dibujar(0));
        r.Observar(60_000, 1_000); // el tramo hasta t=60000 llegó a los 1000 ms locales
        Assert.Equal(60_000 - RelojCarrera.RetrasoMs, r.Dibujar(1_000));
        Assert.Equal(60_500 - RelojCarrera.RetrasoMs, r.Dibujar(1_500));
    }

    [Fact]
    public void UnMensajeQueLlegaTardeMueveElRelojPocoYNoHaciaAtrasDeGolpe()
    {
        var r = new RelojCarrera();
        r.Observar(60_000, 1_000);
        r.Observar(61_000, 2_300); // llegó 300 ms tarde
        var t = r.Dibujar(2_300)!.Value;
        var sinSuavizar = 61_000 - RelojCarrera.RetrasoMs;
        var ideal = 61_300 - RelojCarrera.RetrasoMs;
        Assert.InRange(t, sinSuavizar, ideal); // se movió hacia atrás, pero menos que los 300 ms
        Assert.True(ideal - t < 300 * RelojCarrera.Suavizado + 1);
    }

    [Fact]
    public void UnSaltoGrandeReiniciaElReloj()
    {
        var r = new RelojCarrera();
        r.Observar(60_000, 1_000);
        r.Observar(10_000, 2_000); // otra carrera
        Assert.Equal(10_000 - RelojCarrera.RetrasoMs, r.Dibujar(2_000));
    }
}

public class EstadoVisorPruebas
{
    private static MensajeVisor Pista(string sim = "s1") =>
        new MensajeVisor.Pista(new MensajePista(sim, "9165", "en_curso", 44, 47, [[0, 0], [1000, 0], [1000, 500], [0, 500]]));

    private static MensajeVisor Tramo(long hasta, params Muestra[] muestras) =>
        new MensajeVisor.Coordenadas(new MensajeCoordenadas("s1", "9165", hasta, muestras, null));

    [Fact]
    public void SinPistaOSinCoordenadasNoHayAutos()
    {
        var e = new EstadoVisor();
        e.Aplicar(Tramo(2000, new Muestra(1, 1500, 0, 0)), 0);
        Assert.Empty(e.Autos(0, 0)); // falta la pista
        e.Aplicar(Pista(), 0);
        Assert.NotEmpty(e.Autos(0, 0));
        Assert.StartsWith(EstadoVisor.Numero(Proyeccion.Margen), e.PuntosPista);
    }

    [Fact]
    public void UsaCodigoYColorDePosicionesYPoneAlSeguidoEncima()
    {
        var e = new EstadoVisor();
        e.Aplicar(Pista(), 0);
        e.Aplicar(new MensajeVisor.Posiciones(new FotoPosiciones("9165",
            [new PilotoFoto(1, 44, "HAM", "Lewis HAMILTON", "Mercedes", "#27F4D2"), new PilotoFoto(2, 1, "VER", "Max", "Red Bull", null)], 1, 62)), 0);
        e.Aplicar(Tramo(5000, new Muestra(44, 4000, 0, 0), new Muestra(1, 4000, 100, 0), new Muestra(77, 4000, 50, 0)), 0);

        var autos = e.Autos(0, seguido: 44);
        Assert.Equal(44, autos[^1].Numero);
        Assert.True(autos[^1].Seguido);
        Assert.Equal("#27F4D2", autos[^1].Color);
        Assert.Equal(EstadoVisor.ColorPorDefecto, autos.Single(a => a.Numero == 1).Color);
        Assert.Equal("77", autos.Single(a => a.Numero == 77).Codigo); // sin datos: su número
    }

    [Fact]
    public void LosAutosSeMuevenEntreMuestrasCuadroACuadro()
    {
        var e = new EstadoVisor();
        e.Aplicar(Pista(), 0);
        // Tramo hasta t=10000 recibido a los 0 ms locales: se dibuja t = 10000 - 1600 = 8400.
        e.Aplicar(Tramo(10_000, new Muestra(1, 8_000, 0, 0), new Muestra(1, 9_000, 1000, 0)), 0);
        var x1 = e.Autos(0, 0)[0].X;
        var x2 = e.Autos(100, 0)[0].X; // un cuadro después (100 ms)
        Assert.True(x2 > x1);
        var proy = e.Proyeccion!;
        Assert.Equal(proy.X(400), x1, 3); // 8400 está al 40 % entre 8000 y 9000
    }

    [Fact]
    public void OtraSimulacionOlvidaLosAutosDeLaAnterior()
    {
        var e = new EstadoVisor();
        e.Aplicar(Pista("s1"), 0);
        e.Aplicar(Tramo(2000, new Muestra(1, 1500, 0, 0)), 0);
        e.Aplicar(Pista("s2"), 0);
        Assert.Empty(e.Autos(0, 0));
        Assert.False(e.HayAutos);
    }
}

public class CarreraDemoPruebas
{
    [Fact]
    public void GeneraUnTramoComoElMotor()
    {
        var demo = new CarreraDemo();
        var tramo = demo.Tramo(0, 1000);
        Assert.Equal(1000, tramo.TCarreraMs);
        Assert.All(tramo.Muestras, m => Assert.InRange(m.T, 1, 1000));
        Assert.Equal(8 * 3, tramo.Muestras.Length); // 8 autos × muestras en 270, 540 y 810
        Assert.Equal(8, demo.Posiciones(1000).Pilotos.Length);
    }

    [Fact]
    public void LaDemoPasaPorElMismoCaminoQueLosDatosReales()
    {
        var demo = new CarreraDemo();
        var e = new EstadoVisor();
        e.Aplicar(new MensajeVisor.Pista(demo.Pista()), 0);
        e.Aplicar(new MensajeVisor.Coordenadas(demo.Tramo(0, 3000)), 0);
        var autos = e.Autos(0, 0);
        Assert.Equal(8, autos.Count);
        Assert.All(autos, a =>
        {
            Assert.InRange(a.X, 0, Proyeccion.Ancho);
            Assert.InRange(a.Y, 0, e.Proyeccion!.Alto);
        });
    }
}
