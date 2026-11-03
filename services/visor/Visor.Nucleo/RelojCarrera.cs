// Qué instante de la carrera se dibuja en cada cuadro.

namespace Visor.Nucleo;

/// <summary>
/// El Motor manda las coordenadas en tramos de ~1 s, cada uno con el
/// t_carrera_ms hasta donde llega. El visor dibuja un poco en el pasado
/// (<see cref="RetrasoMs"/>) para tener siempre una muestra antes y otra
/// después del instante dibujado e interpolar entre ellas (RNF-13).
///
/// El reloj estima la diferencia entre el tiempo de carrera y el reloj local, y
/// la suaviza: la red no entrega los mensajes a intervalos exactos y, sin
/// suavizar, los autos darían saltitos hacia atrás.
/// </summary>
public sealed class RelojCarrera
{
    /// <summary>Cuánto en el pasado se dibuja: un tramo (1 s) más margen para la red.</summary>
    public const double RetrasoMs = 1600;
    /// <summary>Peso de cada mensaje nuevo en la estimación (media móvil).</summary>
    public const double Suavizado = 0.15;
    /// <summary>Un salto mayor que esto es otra carrera o una pausa: se reinicia.</summary>
    public const double SaltoMaximoMs = 5000;

    private double? desfase; // tiempo de carrera - reloj local

    /// <summary>true cuando ya llegó al menos un tramo.</summary>
    public bool Listo => desfase.HasValue;

    /// <summary>Registra que el tramo llega hasta <paramref name="tCarreraMs"/>, recibido en <paramref name="ahoraMs"/>.</summary>
    public void Observar(long tCarreraMs, double ahoraMs)
    {
        var nuevo = tCarreraMs - ahoraMs;
        if (desfase is not { } actual || Math.Abs(nuevo - actual) > SaltoMaximoMs)
        {
            desfase = nuevo;
            return;
        }
        // Un mensaje que llegó más rápido que el estimado (desfase mayor) se
        // sigue más deprisa: el retraso de la red solo suma, nunca resta.
        var peso = nuevo > actual ? 0.5 : Suavizado;
        desfase = actual + (nuevo - actual) * peso;
    }

    /// <summary>Instante de carrera a dibujar ahora; null si aún no llegó nada.</summary>
    public double? Dibujar(double ahoraMs) => desfase is { } d ? ahoraMs + d - RetrasoMs : null;

    /// <summary>Olvida lo observado (otra simulación).</summary>
    public void Reiniciar() => desfase = null;
}
