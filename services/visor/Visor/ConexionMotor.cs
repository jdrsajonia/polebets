// WebSocket del visor con el Difusor del Motor, a través del API Gateway.

using System.Net.WebSockets;
using System.Text;

namespace Visor;

/// <summary>Estado de la conexión, para el indicador del visor.</summary>
public enum EstadoConexion { Conectando, Abierta, Reconectando }

/// <summary>
/// Abre su propio WebSocket a /api/ws (Vista C&amp;C: «SPA y Visor → Gateway →
/// Motor»). En el navegador, ClientWebSocket usa el WebSocket del navegador, así
/// que la cookie de sesión viaja sola y el Gateway la valida con auth_request.
/// Si la conexión se cae, reintenta con la misma espera creciente del front
/// (1, 2, 4, 8 y luego cada 10 s).
/// </summary>
public sealed class ConexionMotor(Uri url, Action<string> alMensaje, Action<EstadoConexion> alCambiarEstado)
{
    private static readonly TimeSpan[] Esperas =
        [TimeSpan.FromSeconds(1), TimeSpan.FromSeconds(2), TimeSpan.FromSeconds(4), TimeSpan.FromSeconds(8), TimeSpan.FromSeconds(10)];

    /// <summary>Mantiene la conexión hasta que se cancele <paramref name="ct"/>.</summary>
    public async Task Correr(CancellationToken ct)
    {
        var intentos = 0;
        alCambiarEstado(EstadoConexion.Conectando);
        while (!ct.IsCancellationRequested)
        {
            try
            {
                using var socket = new ClientWebSocket();
                await socket.ConnectAsync(url, ct);
                intentos = 0;
                alCambiarEstado(EstadoConexion.Abierta);
                await Recibir(socket, ct);
            }
            catch (OperationCanceledException) when (ct.IsCancellationRequested)
            {
                return;
            }
            catch (Exception)
            {
                // Sin sesión (401), Motor caído o red cortada: se reintenta igual.
            }
            if (ct.IsCancellationRequested) return;
            alCambiarEstado(EstadoConexion.Reconectando);
            try
            {
                await Task.Delay(Esperas[Math.Min(intentos++, Esperas.Length - 1)], ct);
            }
            catch (OperationCanceledException)
            {
                return;
            }
        }
    }

    /// <summary>Lee mensajes de texto (pueden venir en varios fragmentos) hasta que se cierre.</summary>
    private async Task Recibir(ClientWebSocket socket, CancellationToken ct)
    {
        var buffer = new byte[16 * 1024];
        using var mensaje = new MemoryStream();
        while (socket.State == WebSocketState.Open)
        {
            var r = await socket.ReceiveAsync(buffer, ct);
            if (r.MessageType == WebSocketMessageType.Close) return;
            mensaje.Write(buffer, 0, r.Count);
            if (!r.EndOfMessage) continue;
            if (r.MessageType == WebSocketMessageType.Text)
            {
                alMensaje(Encoding.UTF8.GetString(mensaje.GetBuffer(), 0, (int)mensaje.Length));
            }
            mensaje.SetLength(0);
        }
    }

    /// <summary>URL del WebSocket del mismo origen que la página: ws(s)://host/api/ws.</summary>
    public static Uri UrlPorDefecto(string baseUri)
    {
        var origen = new Uri(baseUri);
        var esquema = origen.Scheme == Uri.UriSchemeHttps ? "wss" : "ws";
        return new Uri($"{esquema}://{origen.Authority}/api/ws");
    }
}
