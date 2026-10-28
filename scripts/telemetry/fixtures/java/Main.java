import com.google.gson.Gson;
import com.google.gson.JsonParser;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.URI;
import java.net.URLDecoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.Executors;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/** Ordinary application APIs only: instrumentation is supplied by RIO. */
public final class Main {
  private static final Logger LOG = LoggerFactory.getLogger(Main.class);
  private static final Gson JSON = new Gson();
  private static final HttpClient CLIENT = HttpClient.newBuilder()
      .connectTimeout(Duration.ofSeconds(5))
      .version(HttpClient.Version.HTTP_1_1)
      .build();

  public static void main(String[] args) throws IOException {
    HttpServer server = HttpServer.create(new InetSocketAddress("0.0.0.0", 8080), 0);
    server.setExecutor(Executors.newFixedThreadPool(8));
    server.createContext("/", Main::handle);
    server.start();
  }

  private static void handle(HttpExchange exchange) throws IOException {
    try {
      if (!exchange.getRequestMethod().equals("GET")) {
        respond(exchange, 405, Map.of("error", "GET required"));
        return;
      }
      String path = exchange.getRequestURI().getPath();
      if (path.equals("/health")) {
        respond(exchange, 200, Map.of("status", "ok"));
        return;
      }
      if (!path.startsWith("/work/") || path.length() == 6) {
        respond(exchange, 404, Map.of("error", "unknown path"));
        return;
      }
      String marker = path.substring(6);
      Thread.sleep(Math.max(0, Math.min(1000, Integer.parseInt(System.getenv().getOrDefault("RIO_FIXTURE_DELAY_MS", "0")))));
      LOG.info("rio-fixture {}", marker);
      Map<String, Object> response = new LinkedHashMap<>();
      response.put("marker", marker);
      response.put("language", "java");
      Map<String, Object> headers = new LinkedHashMap<>();
      exchange.getRequestHeaders().forEach((name, values) ->
          headers.put(name.toLowerCase(Locale.ROOT), values));
      response.put("headers", headers);

      String next = queryValue(exchange.getRequestURI().getRawQuery(), "next");
      if (next != null) {
        // Do not copy incoming headers: the automatic agent must inject context.
        HttpRequest request = HttpRequest.newBuilder(URI.create(next))
            .timeout(Duration.ofSeconds(8)).GET().build();
        HttpResponse<String> downstream = CLIENT.send(request, HttpResponse.BodyHandlers.ofString());
        if (downstream.statusCode() < 200 || downstream.statusCode() >= 300) {
          throw new IOException("Downstream HTTP " + downstream.statusCode());
        }
        response.put("downstream", JsonParser.parseString(downstream.body()));
      }
      respond(exchange, 200, response);
    } catch (Exception error) {
      if (error instanceof InterruptedException) Thread.currentThread().interrupt();
      respond(exchange, 502, Map.of("error", error.toString()));
    } finally {
      exchange.close();
    }
  }

  private static String queryValue(String query, String key) {
    if (query == null) return null;
    for (String parameter : query.split("&")) {
      String[] pair = parameter.split("=", 2);
      if (URLDecoder.decode(pair[0], StandardCharsets.UTF_8).equals(key)) {
        return pair.length == 2 ? URLDecoder.decode(pair[1], StandardCharsets.UTF_8) : "";
      }
    }
    return null;
  }

  private static void respond(HttpExchange exchange, int status, Object body) throws IOException {
    byte[] bytes = JSON.toJson(body).getBytes(StandardCharsets.UTF_8);
    exchange.getResponseHeaders().set("Content-Type", "application/json");
    exchange.sendResponseHeaders(status, bytes.length);
    exchange.getResponseBody().write(bytes);
  }
}
