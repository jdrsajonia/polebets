// Estado del visor: lo que ya llegó del Motor y qué dibujar en cada cuadro.

using System.Globalization;
using System.Text;

namespace Visor.Nucleo;

/// <summary>Un auto listo para dibujar, ya en coordenadas del lienzo.</summary>
public sealed record AutoEnPista(int Numero, string Codigo, string Color, int? Posicion, double X, double Y, bool Seguido);

/// <summary>
/// Junta la pista, los datos de los pilotos (de "posiciones") y las coordenadas,
/// y arma cada cuadro. No sabe de Blazor ni de la red: el componente le pasa
/// los mensajes y la hora local, y le pide los autos a dibujar.
/// </summary>
public sealed class EstadoVisor
{
    /// <summary>Color si "posiciones" no trae el del equipo.</summary>
    public const string ColorPorDefecto = "#A1A1AA";

    private readonly Interpolador interpolador = new();
    private readonly RelojCarrera reloj = new();
    private Dictionary<int, PilotoFoto> pilotos = new();
    private string? simulacionId;

    /// <summary>Proyección de la pista; null hasta que llega "pista".</summary>
    public Proyeccion? Proyeccion { get; private set; }

    /// <summary>Atributo points de la polilínea SVG de la pista (cerrada).</summary>
    public string PuntosPista { get; private set; } = "";

    /// <summary>Primer punto de la pista en el lienzo (la línea de meta aproximada).</summary>
    public (double X, double Y) Meta { get; private set; }

    /// <summary>Estado de la simulación según la última pista ("programada" o "en_curso").</summary>
    public string? EstadoCarrera { get; private set; }

    /// <summary>true cuando ya llegaron coordenadas y hay autos que mover.</summary>
    public bool HayAutos => reloj.Listo && interpolador.Autos.Any();

    /// <summary>Aplica un mensaje del Motor.</summary>
    public void Aplicar(MensajeVisor mensaje, double ahoraMs)
    {
        switch (mensaje)
        {
            case MensajeVisor.Pista p:
                AplicarPista(p.Datos);
                break;
            case MensajeVisor.Coordenadas c:
                AplicarCoordenadas(c.Datos, ahoraMs);
                break;
            case MensajeVisor.Posiciones f:
                pilotos = f.Datos.Pilotos.ToDictionary(x => x.Numero);
                break;
        }
    }

    /// <summary>Dibuja la pista; si es de otra simulación, olvida los autos de la anterior.</summary>
    private void AplicarPista(MensajePista p)
    {
        CambiarSimulacion(p.SimulacionId);
        EstadoCarrera = p.Estado;
        if (p.Puntos.Length == 0) return;
        var proyeccion = new Proyeccion(p.Puntos);
        var sb = new StringBuilder();
        foreach (var punto in p.Puntos.Append(p.Puntos[0])) // cerrada: vuelve al inicio
        {
            sb.Append(Numero(proyeccion.X(punto[0]))).Append(',').Append(Numero(proyeccion.Y(punto[1]))).Append(' ');
        }
        Proyeccion = proyeccion;
        PuntosPista = sb.ToString().TrimEnd();
        Meta = (proyeccion.X(p.Puntos[0][0]), proyeccion.Y(p.Puntos[0][1]));
    }

    /// <summary>Guarda las muestras del tramo y ajusta el reloj.</summary>
    private void AplicarCoordenadas(MensajeCoordenadas c, double ahoraMs)
    {
        CambiarSimulacion(c.SimulacionId);
        EstadoCarrera = "en_curso";
        foreach (var m in c.Muestras) interpolador.Agregar(m);
        reloj.Observar(c.TCarreraMs, ahoraMs);
    }

    /// <summary>Si llega otra simulación, los autos y el reloj de la anterior ya no sirven.</summary>
    private void CambiarSimulacion(string id)
    {
        if (id == simulacionId) return;
        simulacionId = id;
        interpolador.Limpiar();
        reloj.Reiniciar();
    }

    /// <summary>
    /// Autos a dibujar en <paramref name="ahoraMs"/>. El seguido va al final para
    /// quedar encima de los demás.
    /// </summary>
    public IReadOnlyList<AutoEnPista> Autos(double ahoraMs, int seguido)
    {
        if (Proyeccion is not { } proy || reloj.Dibujar(ahoraMs) is not { } t) return [];
        interpolador.Recortar(t);
        var autos = new List<AutoEnPista>();
        foreach (var numero in interpolador.Autos)
        {
            if (interpolador.PosicionEn(numero, t) is not { } pos) continue;
            pilotos.TryGetValue(numero, out var p);
            autos.Add(new AutoEnPista(
                numero,
                p?.Codigo is { Length: > 0 } codigo ? codigo : numero.ToString(CultureInfo.InvariantCulture),
                p?.ColorEquipo is { Length: > 0 } color ? color : ColorPorDefecto,
                p?.Posicion,
                proy.X(pos.X),
                proy.Y(pos.Y),
                numero == seguido));
        }
        // Del último al primero, y el seguido encima de todos.
        return autos.OrderBy(a => a.Seguido).ThenByDescending(a => a.Posicion ?? int.MaxValue).ToList();
    }

    /// <summary>Número con un decimal y punto, para los atributos SVG.</summary>
    public static string Numero(double valor) => valor.ToString("0.#", CultureInfo.InvariantCulture);
}
