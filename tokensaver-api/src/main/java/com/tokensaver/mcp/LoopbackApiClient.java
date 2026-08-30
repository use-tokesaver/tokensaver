package com.tokensaver.mcp;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.tokensaver.common.ApiException;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;

/**
 * Calls this same service's own REST API over loopback HTTP (http://localhost:&lt;port&gt;),
 * so the MCP tool layer has exactly one real implementation to go through — the REST
 * controllers — rather than a second, separately-maintained path into the service beans.
 *
 * Only JSON-bodied endpoints are called here: MCP tools are deliberately limited to
 * operations with no file content (see McpToolsConfiguration), so there's no multipart
 * traffic to support.
 */
@Component
class LoopbackApiClient {

    private final String baseUrl;
    private final ObjectMapper mapper;
    private final HttpClient httpClient = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(10))
            .build();

    LoopbackApiClient(@Value("${server.port:8080}") String serverPort, ObjectMapper mapper) {
        this.baseUrl = "http://localhost:" + serverPort;
        this.mapper = mapper;
    }

    JsonNode postJson(String path, Object requestBody) {
        try {
            String json = mapper.writeValueAsString(requestBody);
            HttpRequest request = HttpRequest.newBuilder(URI.create(baseUrl + path))
                    .header("Content-Type", "application/json")
                    .POST(HttpRequest.BodyPublishers.ofString(json, StandardCharsets.UTF_8))
                    .build();
            HttpResponse<byte[]> response = httpClient.send(request, HttpResponse.BodyHandlers.ofByteArray());
            checkStatus(response);
            return mapper.readTree(response.body());
        } catch (IOException | InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new ApiException("Internal call to " + path + " failed: " + e.getMessage(), e);
        }
    }

    private void checkStatus(HttpResponse<byte[]> response) {
        if (response.statusCode() >= 200 && response.statusCode() < 300) {
            return;
        }
        String message;
        try {
            JsonNode errorBody = mapper.readTree(response.body());
            message = errorBody.has("error") ? errorBody.get("error").asText() : errorBody.toString();
        } catch (IOException e) {
            message = new String(response.body(), StandardCharsets.UTF_8);
        }
        throw new ApiException("tokensaver API returned " + response.statusCode() + ": " + message);
    }
}
