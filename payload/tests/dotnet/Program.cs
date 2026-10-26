var loaded = AppDomain.CurrentDomain.GetAssemblies()
    .Select(assembly => assembly.GetName().Name).ToArray();
if (!loaded.Contains("OpenTelemetry.AutoInstrumentation"))
    throw new Exception("The .NET auto-instrumentation did not initialize: " + string.Join(", ", loaded));
Console.WriteLine(".NET Injector activated");
