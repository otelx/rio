using System.Text.Json;

var builder = WebApplication.CreateBuilder(args);
builder.Logging.AddFilter("Microsoft", LogLevel.Warning);
builder.Services.AddHttpClient("downstream", client =>
{
    client.Timeout = TimeSpan.FromSeconds(10);
});

var app = builder.Build();
var logger = app.Services.GetRequiredService<ILoggerFactory>().CreateLogger("RioFixture");

app.MapGet("/health", () => Results.Json(new { status = "ok", language = "dotnet" }));
app.MapGet("/work/{marker}", async (string marker, HttpContext context, IHttpClientFactory clients) =>
{
    // These are normal application logs. RIO's shipped agent supplies the bridge.
    await Task.Delay(Math.Clamp(int.Parse(Environment.GetEnvironmentVariable("RIO_FIXTURE_DELAY_MS") ?? "0"), 0, 1000));
    logger.LogInformation("rio-fixture {Marker}", marker);
    var headers = context.Request.Headers.ToDictionary(
        header => header.Key.ToLowerInvariant(),
        header => header.Value.Select(value => value ?? "").ToArray());

    JsonElement? downstream = null;
    var next = context.Request.Query["next"].ToString();
    if (next.Length != 0)
    {
        // Do not copy received context: HttpClient instrumentation must inject it.
        using var response = await clients.CreateClient("downstream").GetAsync(next, context.RequestAborted);
        response.EnsureSuccessStatusCode();
        downstream = await response.Content.ReadFromJsonAsync<JsonElement>(context.RequestAborted);
    }

    return Results.Json(new { marker, language = "dotnet", headers, downstream });
});

app.Run();
