// Pasa las coordenadas de OpenF1 al lienzo SVG del visor.

namespace Visor.Nucleo;

/// <summary>
/// Ajusta el rectángulo que ocupa la pista a un lienzo de <see cref="Ancho"/>
/// de ancho, con margen y sin deformarla. En OpenF1 la y crece hacia arriba y
/// en SVG hacia abajo: por eso se invierte.
/// </summary>
public sealed class Proyeccion
{
    /// <summary>Ancho fijo del lienzo (viewBox); el alto sale de la forma de la pista.</summary>
    public const double Ancho = 1000;
    /// <summary>Margen alrededor de la pista, para que los autos y sus códigos no se corten.</summary>
    public const double Margen = 40;
    /// <summary>Límites del alto, para pistas muy anchas o muy altas.</summary>
    public const double AltoMinimo = 400, AltoMaximo = 900;

    private readonly double minX, maxY, escala, desplazamientoX, desplazamientoY;

    /// <summary>Alto del lienzo (viewBox).</summary>
    public double Alto { get; }

    /// <summary>Calcula la proyección a partir de los puntos de la pista.</summary>
    public Proyeccion(IReadOnlyCollection<double[]> puntos)
    {
        if (puntos.Count == 0) throw new ArgumentException("La pista no tiene puntos", nameof(puntos));
        minX = puntos.Min(p => p[0]);
        var maxX = puntos.Max(p => p[0]);
        var minY = puntos.Min(p => p[1]);
        maxY = puntos.Max(p => p[1]);
        var anchoPista = Math.Max(maxX - minX, 1);
        var altoPista = Math.Max(maxY - minY, 1);

        var util = Ancho - 2 * Margen;
        Alto = Math.Clamp(util * altoPista / anchoPista + 2 * Margen, AltoMinimo, AltoMaximo);
        // La escala que quepa en ambos sentidos; lo que sobre se reparte para centrar.
        escala = Math.Min(util / anchoPista, (Alto - 2 * Margen) / altoPista);
        desplazamientoX = (Ancho - anchoPista * escala) / 2;
        desplazamientoY = (Alto - altoPista * escala) / 2;
    }

    /// <summary>Coordenada x de OpenF1 → x del lienzo.</summary>
    public double X(double x) => desplazamientoX + (x - minX) * escala;

    /// <summary>Coordenada y de OpenF1 → y del lienzo (invertida).</summary>
    public double Y(double y) => desplazamientoY + (maxY - y) * escala;
}
