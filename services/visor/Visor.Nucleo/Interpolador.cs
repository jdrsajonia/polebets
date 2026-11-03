// Posición de cada auto entre muestras.

namespace Visor.Nucleo;

/// <summary>
/// Guarda las muestras recientes de cada auto (ordenadas por tiempo de
/// carrera) y da su posición en cualquier instante, interpolando en línea recta
/// entre la muestra anterior y la siguiente. OpenF1 trae ~3,7 muestras por
/// segundo; el visor dibuja a 30 cuadros o más, así que la mayoría de los
/// cuadros caen entre dos muestras.
/// </summary>
public sealed class Interpolador
{
    /// <summary>Muestras que se conservan detrás del instante dibujado.</summary>
    public const long HistoriaMs = 5000;

    private readonly Dictionary<int, List<Muestra>> porAuto = new();

    /// <summary>Números de los autos con muestras.</summary>
    public IEnumerable<int> Autos => porAuto.Keys;

    /// <summary>Agrega una muestra en su lugar; una repetida (mismo auto y t) se ignora.</summary>
    public void Agregar(Muestra m)
    {
        if (!porAuto.TryGetValue(m.Numero, out var lista))
        {
            porAuto[m.Numero] = lista = new List<Muestra>();
        }
        // Casi siempre llega al final: se busca desde atrás.
        var i = lista.Count;
        while (i > 0 && lista[i - 1].T > m.T) i--;
        if (i > 0 && lista[i - 1].T == m.T) return;
        lista.Insert(i, m);
    }

    /// <summary>Posición (x, y) del auto en <paramref name="t"/>; null si no tiene muestras.</summary>
    /// <remarks>Antes de la primera muestra queda en la primera; después de la última, en la última.</remarks>
    public (double X, double Y)? PosicionEn(int numero, double t)
    {
        if (!porAuto.TryGetValue(numero, out var lista) || lista.Count == 0) return null;
        if (t <= lista[0].T) return (lista[0].X, lista[0].Y);
        var ultima = lista[^1];
        if (t >= ultima.T) return (ultima.X, ultima.Y);

        // Búsqueda binaria de la última muestra con T <= t.
        int bajo = 0, alto = lista.Count - 1;
        while (bajo < alto)
        {
            var medio = (bajo + alto + 1) / 2;
            if (lista[medio].T <= t) bajo = medio; else alto = medio - 1;
        }
        var a = lista[bajo];
        var b = lista[bajo + 1];
        var f = (t - a.T) / (b.T - a.T);
        return (a.X + (b.X - a.X) * f, a.Y + (b.Y - a.Y) * f);
    }

    /// <summary>Descarta lo que quedó más de <see cref="HistoriaMs"/> detrás de <paramref name="t"/> (deja una para interpolar).</summary>
    public void Recortar(double t)
    {
        var limite = t - HistoriaMs;
        foreach (var lista in porAuto.Values)
        {
            var sobran = 0;
            while (sobran < lista.Count - 1 && lista[sobran + 1].T < limite) sobran++;
            if (sobran > 0) lista.RemoveRange(0, sobran);
        }
    }

    /// <summary>Olvida todas las muestras (otra simulación).</summary>
    public void Limpiar() => porAuto.Clear();

    /// <summary>Cuántas muestras tiene un auto (para las pruebas).</summary>
    public int Cantidad(int numero) => porAuto.TryGetValue(numero, out var l) ? l.Count : 0;
}
