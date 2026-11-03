// Carrera de mentira para desarrollar el visor sin el stack (atributo demo="true").

using System.Globalization;

namespace Visor.Nucleo;

/// <summary>
/// Genera los mismos mensajes que el Motor ("pista", "posiciones" y
/// "coordenadas" en tramos de 1 s con muestras cada 270 ms) sobre la pista del
/// mockup, con 8 autos a ritmos distintos. Así el modo demo pasa por el mismo
/// camino que los datos reales: interpolación, reloj y proyección.
/// </summary>
public sealed class CarreraDemo
{
    /// <summary>Puntos del trazado del mockup (en su lienzo de 800×480, y hacia abajo).</summary>
    private static readonly double[][] Trazado =
    [
        [160, 410], [300, 415], [440, 418], [560, 416], [640, 405], [690, 375], [705, 330], [690, 290],
        [650, 268], [600, 262], [560, 245], [545, 210], [565, 175], [610, 150], [640, 115], [630, 80],
        [585, 62], [520, 60], [455, 72], [400, 98], [350, 120], [295, 118], [245, 100], [195, 92],
        [145, 108], [110, 145], [98, 195], [112, 240], [150, 265], [190, 290], [195, 330], [170, 365], [150, 390],
    ];

    private static readonly (int Numero, string Codigo, string Color, double Ritmo)[] Autos =
    [
        (4, "VAR", "#E10600", 1.000), (16, "OKA", "#C0C4CC", 0.997), (55, "SAN", "#F59E0B", 0.995),
        (11, "RIO", "#38BDF8", 0.993), (63, "LIN", "#A78BFA", 0.990), (23, "NAV", "#22C55E", 0.988),
        (27, "BRA", "#F472B6", 0.985), (31, "DUV", "#FACC15", 0.982),
    ];

    /// <summary>Segundos que tarda el más rápido en dar una vuelta.</summary>
    public const double VueltaS = 40;
    /// <summary>Cada cuánto hay una muestra por auto (como OpenF1, ~3,7 Hz).</summary>
    public const long PasoMuestraMs = 270;

    private const string Simulacion = "demo";
    private readonly double[] acumulado;
    private readonly double largo;

    /// <summary>Precalcula el largo de la pista para recorrerla a velocidad constante.</summary>
    public CarreraDemo()
    {
        acumulado = new double[Trazado.Length + 1];
        for (var i = 1; i <= Trazado.Length; i++)
        {
            var a = Trazado[i - 1];
            var b = Trazado[i % Trazado.Length];
            acumulado[i] = acumulado[i - 1] + Math.Sqrt(Math.Pow(b[0] - a[0], 2) + Math.Pow(b[1] - a[1], 2));
        }
        largo = acumulado[^1];
    }

    /// <summary>La pista, con la y hacia arriba como OpenF1.</summary>
    public MensajePista Pista() =>
        new(Simulacion, "demo", "en_curso", 4, 1, Trazado.Select(p => new[] { p[0], -p[1] }).ToArray());

    /// <summary>Posiciones según cuánto ha recorrido cada auto en <paramref name="tMs"/>.</summary>
    public FotoPosiciones Posiciones(long tMs)
    {
        var orden = Autos.OrderByDescending(a => Recorrido(a.Ritmo, a.Numero, tMs)).ToArray();
        var filas = orden.Select((a, i) => new PilotoFoto(i + 1, a.Numero, a.Codigo, a.Codigo, "Demo", a.Color)).ToArray();
        return new FotoPosiciones("demo", filas, (int)(Recorrido(1, 0, tMs) / largo) + 1, 53);
    }

    /// <summary>Muestras de todos los autos con t en (desde, hasta], como un tramo del Motor.</summary>
    public MensajeCoordenadas Tramo(long desde, long hasta)
    {
        var muestras = new List<Muestra>();
        for (var t = (desde / PasoMuestraMs + 1) * PasoMuestraMs; t <= hasta; t += PasoMuestraMs)
        {
            foreach (var a in Autos)
            {
                var (x, y) = PuntoEn(Recorrido(a.Ritmo, a.Numero, t) % largo);
                muestras.Add(new Muestra(a.Numero, t, x, -y));
            }
        }
        return new MensajeCoordenadas(Simulacion, "demo", hasta, muestras.ToArray(), null);
    }

    /// <summary>Distancia recorrida: ritmo propio, un pequeño vaivén y la salida escalonada.</summary>
    private double Recorrido(double ritmo, int numero, long tMs)
    {
        var s = tMs / 1000.0;
        var salida = -numero * 0.004 * largo;
        return salida + largo * ritmo * s / VueltaS + 6 * Math.Sin(s * 0.4 + numero);
    }

    /// <summary>Punto de la pista a una distancia dada desde el inicio.</summary>
    private (double X, double Y) PuntoEn(double distancia)
    {
        if (distancia < 0) distancia += largo;
        var i = Array.FindLastIndex(acumulado, d => d <= distancia);
        i = Math.Clamp(i, 0, Trazado.Length - 1);
        var a = Trazado[i];
        var b = Trazado[(i + 1) % Trazado.Length];
        var f = (distancia - acumulado[i]) / (acumulado[i + 1] - acumulado[i]);
        return (a[0] + (b[0] - a[0]) * f, a[1] + (b[1] - a[1]) * f);
    }

    /// <summary>Texto para depurar.</summary>
    public override string ToString() => string.Create(CultureInfo.InvariantCulture, $"CarreraDemo({Autos.Length} autos, {largo:0} u)");
}
