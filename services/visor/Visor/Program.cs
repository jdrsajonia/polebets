// Arranque del visor: registra el componente como el elemento HTML <visor-pista>,
// así cualquier página (el Front en React) lo incrusta sin saber de Blazor.

using Microsoft.AspNetCore.Components.Web;
using Microsoft.AspNetCore.Components.WebAssembly.Hosting;
using Visor;

var builder = WebAssemblyHostBuilder.CreateDefault(args);
builder.RootComponents.RegisterCustomElement<VisorPista>("visor-pista");
await builder.Build().RunAsync();
