// Mensajes del WebSocket del Motor que usa el visor (services/motor/README.md).
// Todos llegan en el sobre común { "type": ..., "payload": ... }.

using System.Text.Json;
using System.Text.Json.Serialization;

namespace Visor.Nucleo;

/// <summary>"pista": el trazado de una vuelta completa (HU2-15).</summary>
public sealed record MensajePista(
    string SimulacionId,
    string CarreraId,
    string Estado,
    int PilotoReferencia,
    int VueltaReferencia,
    double[][] Puntos);

/// <summary>"coordenadas": las muestras x/y del último tramo de carrera.</summary>
public sealed record MensajeCoordenadas(
    string SimulacionId,
    string CarreraId,
    long TCarreraMs,
    Muestra[] Muestras,
    DateTimeOffset? EmitidoEn);

/// <summary>Posición x/y de un auto en un instante del tiempo de carrera.</summary>
public sealed record Muestra(int Numero, long T, double X, double Y);

/// <summary>"posiciones" (HU2-14): de aquí salen código, color y puesto de cada auto.</summary>
public sealed record FotoPosiciones(string CarreraId, PilotoFoto[] Pilotos, int? Vuelta, int? TotalVueltas);

/// <summary>Un renglón de la tabla de posiciones.</summary>
public sealed record PilotoFoto(int Posicion, int Numero, string Codigo, string Nombre, string Equipo, string? ColorEquipo);

/// <summary>Lo que el visor entiende de un mensaje; los demás tipos dan <c>null</c>.</summary>
public abstract record MensajeVisor
{
    public sealed record Pista(MensajePista Datos) : MensajeVisor;
    public sealed record Coordenadas(MensajeCoordenadas Datos) : MensajeVisor;
    public sealed record Posiciones(FotoPosiciones Datos) : MensajeVisor;

    /// <summary>Lee un mensaje del Motor. Devuelve null si no es para el visor o está mal formado.</summary>
    public static MensajeVisor? Leer(string texto)
    {
        try
        {
            using var doc = JsonDocument.Parse(texto);
            if (!doc.RootElement.TryGetProperty("type", out var tipo) ||
                !doc.RootElement.TryGetProperty("payload", out var payload))
            {
                return null;
            }
            return tipo.GetString() switch
            {
                "pista" => Crear(payload, ContextoJson.Default.MensajePista, d => new Pista(d)),
                "coordenadas" => Crear(payload, ContextoJson.Default.MensajeCoordenadas, d => new Coordenadas(d)),
                "posiciones" => Crear(payload, ContextoJson.Default.FotoPosiciones, d => new Posiciones(d)),
                _ => null,
            };
        }
        catch (JsonException)
        {
            return null;
        }
    }

    /// <summary>Deserializa el payload con el contexto generado (sin reflexión, apto para recortar).</summary>
    private static MensajeVisor? Crear<T>(JsonElement payload, System.Text.Json.Serialization.Metadata.JsonTypeInfo<T> info, Func<T, MensajeVisor> envolver)
        where T : class
    {
        var datos = payload.Deserialize(info);
        return datos is null ? null : envolver(datos);
    }
}

/// <summary>Serialización generada en compilación: nombres en snake_case como los manda el Motor.</summary>
[JsonSourceGenerationOptions(PropertyNamingPolicy = JsonKnownNamingPolicy.SnakeCaseLower)]
[JsonSerializable(typeof(MensajePista))]
[JsonSerializable(typeof(MensajeCoordenadas))]
[JsonSerializable(typeof(FotoPosiciones))]
internal sealed partial class ContextoJson : JsonSerializerContext;
