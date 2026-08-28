package com.tokensaver.mcp;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.tokensaver.common.ApiException;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.List;
import java.util.Map;

/**
 * Calls this same service's own REST API over loopback HTTP (http://localhost:&lt;port&gt;),
 * so the MCP tool layer has exactly one real implementation to go through — the REST
 * controllers — rather than a second, separately-maintained path into the service beans.
 */
@Component
class LoopbackApiClient {

    private final String baseUrl;
    private final ObjectMapper mapper;
    private final HttpClient httpClient = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(10))
            .build();
    private final String boundary = "----TokenSaverBoundary" + System.nanoTime();

    LoopbackApiClient(@Value("${server.port:8080}") String serverPort, ObjectMapper mapper) {
        this.baseUrl = "http://localhost:" + serverPort;
        this.mapper = mapper;
    }

    record MultipartPart(String fieldName, String filename, byte[] bytes) {
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

    JsonNode postMultipartForJson(String path, String query, List<MultipartPart> parts) {
        HttpResponse<byte[]> response = sendMultipart(path, query, parts);
        checkStatus(response);
        try {
            return mapper.readTree(response.body());
        } catch (IOException e) {
            throw new ApiException("Failed to parse response from " + path + ": " + e.getMessage(), e);
        }
    }

    byte[] postMultipartForBytes(String path, String query, List<MultipartPart> parts) {
        HttpResponse<byte[]> response = sendMultipart(path, query, parts);
        checkStatus(response);
        return response.body();
    }

    private HttpResponse<byte[]> sendMultipart(String path, String query, List<MultipartPart> parts) {
        byte[] body = buildMultipartBody(parts);
        String url = baseUrl + path + (query == null || query.isBlank() ? "" : "?" + query);
        HttpRequest request = HttpRequest.newBuilder(URI.create(url))
                .header("Content-Type", "multipart/form-data; boundary=" + boundary)
                .POST(HttpRequest.BodyPublishers.ofByteArray(body))
                .build();
        try {
            return httpClient.send(request, HttpResponse.BodyHandlers.ofByteArray());
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

    private byte[] buildMultipartBody(List<MultipartPart> parts) {
        try {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            for (MultipartPart part : parts) {
                out.write(("--" + boundary + "\r\n").getBytes(StandardCharsets.UTF_8));
                out.write(("Content-Disposition: form-data; name=\"" + part.fieldName()
                        + "\"; filename=\"" + part.filename() + "\"\r\n").getBytes(StandardCharsets.UTF_8));
                out.write("Content-Type: application/octet-stream\r\n\r\n".getBytes(StandardCharsets.UTF_8));
                out.write(part.bytes());
                out.write("\r\n".getBytes(StandardCharsets.UTF_8));
            }
            out.write(("--" + boundary + "--\r\n").getBytes(StandardCharsets.UTF_8));
            return out.toByteArray();
        } catch (IOException e) {
            throw new ApiException("Failed to build multipart request: " + e.getMessage(), e);
        }
    }

    static String urlEncode(String value) {
        return URLEncoder.encode(value, StandardCharsets.UTF_8);
    }

    static String buildQuery(Map<String, String> params) {
        StringBuilder sb = new StringBuilder();
        for (Map.Entry<String, String> entry : params.entrySet()) {
            if (entry.getValue() == null) {
                continue;
            }
            if (!sb.isEmpty()) {
                sb.append("&");
            }
            sb.append(urlEncode(entry.getKey())).append("=").append(urlEncode(entry.getValue()));
        }
        return sb.toString();
    }
}
