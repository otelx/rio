using System.Reflection;

// Keep this bootstrap BCL-only. The upstream hook must establish its assembly
// resolver before any OpenTelemetry or DiagnosticSource types are loaded.
internal static class StartupHook
{
    private const string Plugin = "Rio.PropagationPlugin, Rio.Propagation";

    public static void Initialize()
    {
        MethodInfo initialize;
        try
        {
            if (Environment.Version.Major >= 8) ConfigurePlugins();
            var directory = Path.GetDirectoryName(Assembly.GetExecutingAssembly().Location)!;
            var original = Assembly.LoadFrom(Path.Combine(directory,
                "OpenTelemetry.AutoInstrumentation.StartupHook.original.dll"));
            initialize = original.GetType("StartupHook", throwOnError: true)!
                .GetMethod("Initialize", BindingFlags.Static | BindingFlags.Public)
                ?? throw new MissingMethodException("Upstream StartupHook.Initialize is missing");
        }
        catch (Exception error)
        {
            Console.Error.WriteLine("[rio-dotnet-bootstrap] Initialization failed: " + error);
            if (bool.TryParse(Environment.GetEnvironmentVariable("OTEL_DOTNET_AUTO_FAIL_FAST_ENABLED"), out var failFast) && failFast)
                throw;
            return;
        }

        // Preserve the original startup/application exception instead of adding
        // TargetInvocationException around the upstream initialization path.
        initialize.Invoke(null, BindingFlags.DoNotWrapExceptions, null, null, null);
    }

    private static void ConfigurePlugins()
    {
        const string variable = "OTEL_DOTNET_AUTO_PLUGINS";
        var configured = Environment.GetEnvironmentVariable(variable);
        var alreadyPresent = false;
        foreach (var value in (configured ?? "").Split(':'))
        {
            var plugin = value.Trim();
            alreadyPresent |= plugin == Plugin || plugin.StartsWith(Plugin + ",", StringComparison.Ordinal);
        }
        if (!alreadyPresent)
        {
            Environment.SetEnvironmentVariable(variable,
                string.IsNullOrWhiteSpace(configured) ? Plugin : Plugin + ":" + configured);
        }
    }
}
