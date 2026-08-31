package com.tokensaver.mcp;

import com.fasterxml.jackson.databind.JsonNode;
import com.tokensaver.common.ApiException;
import io.modelcontextprotocol.json.McpJsonMapper;
import io.modelcontextprotocol.json.McpJsonMapperSupplier;
import io.modelcontextprotocol.server.McpServer;
import io.modelcontextprotocol.server.McpServerFeatures;
import io.modelcontextprotocol.server.McpSyncServer;
import io.modelcontextprotocol.server.transport.HttpServletStreamableServerTransportProvider;
import io.modelcontextprotocol.spec.McpSchema;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.web.servlet.ServletRegistrationBean;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.ServiceLoader;

/**
 * Exposes tokensaver's REST API as native MCP tools over HTTP at /mcp (Streamable HTTP
 * transport), so an MCP-capable agent can add this deployment as a remote connector —
 * the same way a Google Drive/Gmail connector works — with nothing to download or run
 * locally.
 *
 * Every tool handler here forwards to the matching REST endpoint via {@link LoopbackApiClient}
 * rather than calling service beans directly, so the REST controllers stay the single real
 * implementation and this layer is just a thin protocol adapter in front of them.
 *
 * File-based operations (extract-text, image convert, OCR, zip/unzip, audio transcription,
 * HTML/Markdown rendering, barcode generate/decode, PDF merge/split/rotate/watermark/
 * fill-form) are deliberately NOT exposed as MCP tools. MCP tool arguments are JSON, which
 * has no binary type, so the only way to pass file content through a tool call is base64 —
 * and that forces the model to generate the entire file as output tokens just to make the
 * call, which is more expensive than the script this API exists to replace. Rather than
 * rely on an agent reading a warning before reaching for that option, those endpoints
 * simply aren't offered as tools at all; they're REST + curl only (see the README).
 */
@Configuration
public class McpToolsConfiguration {

    private static final Logger log = LoggerFactory.getLogger("com.tokensaver.mcp");

    private final LoopbackApiClient client;
    private final String instructions;

    public McpToolsConfiguration(
            LoopbackApiClient client,
            @Value("${tokensaver.public-base-url:http://localhost:${server.port:8080}}") String publicBaseUrl) {
        this.client = client;
        this.instructions = """
                tokensaver provides deterministic-task tools so you don't have to write \
                and debug a throwaway script for common jobs.

                This server's base URL is: %s

                File-based operations (extracting text from documents, OCR, image convert, \
                zip/unzip) are NOT available as tools here — call the REST API directly with \
                curl instead, e.g.:
                  curl -F "file=@/path/to/file.pdf" %s/api/files/extract-text
                Do not probe for a health-check endpoint or otherwise try to discover the base \
                URL first — it is given above. Do not attempt to read such a file and pass its \
                content to a tool call; no such tool is offered, and doing so would just mean \
                generating the file as output tokens for nothing.

                For everything below, the MCP tool call is always the right choice — call it \
                directly instead of writing a script or using curl.
                """.formatted(publicBaseUrl, publicBaseUrl);
    }

    @Bean
    public HttpServletStreamableServerTransportProvider mcpTransportProvider() {
        McpJsonMapper jsonMapper = ServiceLoader.load(McpJsonMapperSupplier.class)
                .findFirst()
                .orElseThrow(() -> new IllegalStateException("No McpJsonMapperSupplier found on classpath"))
                .get();

        return HttpServletStreamableServerTransportProvider.builder()
                .jsonMapper(jsonMapper)
                .mcpEndpoint("/mcp")
                .build();
    }

    /** Also injectable elsewhere (e.g. {@link McpToolsInfoController}) to read back the same tool list. */
    @Bean
    public McpSyncServer mcpSyncServer(HttpServletStreamableServerTransportProvider transportProvider) {
        McpSyncServer server = McpServer.sync(transportProvider)
                .serverInfo("tokensaver", "0.1.0-POC")
                .instructions(instructions)
                .capabilities(McpSchema.ServerCapabilities.builder().tools(true).build())
                .tools(
                        webExtractTool(),
                        convertDataTool(),
                        diffDataTool(),
                        hashTextTool(),
                        base64Tool(),
                        evaluateFormulaTool())
                .build();
        Runtime.getRuntime().addShutdownHook(new Thread(server::close));
        return server;
    }

    @Bean
    public ServletRegistrationBean<HttpServletStreamableServerTransportProvider> mcpServlet(
            HttpServletStreamableServerTransportProvider transportProvider, McpSyncServer mcpSyncServer) {
        // mcpSyncServer is an unused parameter here on purpose: depending on it forces Spring to
        // build it (which attaches the tool list to transportProvider) before this servlet registers.
        return new ServletRegistrationBean<>(transportProvider, "/mcp", "/mcp/*");
    }

    private McpServerFeatures.SyncToolSpecification webExtractTool() {
        McpSchema.Tool tool = McpSchema.Tool.builder("web_extract")
                .description("Fetch a web page and return its boilerplate-stripped title/text/markdown, "
                        + "instead of writing a scrape-and-clean script.")
                .inputSchema(Map.of(
                        "type", "object",
                        "properties", Map.of("url", Map.of("type", "string", "description", "http(s) URL to fetch")),
                        "required", List.of("url")))
                .build();
        return toolSpec(tool, args -> {
            JsonNode result = client.postJson("/api/web/extract", Map.of("url", requireString(args, "url")));
            return "Title: " + result.path("title").asText() + "\n\n" + result.path("markdown").asText();
        });
    }

    private McpServerFeatures.SyncToolSpecification convertDataTool() {
        McpSchema.Tool tool = McpSchema.Tool.builder("convert_data")
                .description("Convert structured data between json, yaml, and csv, "
                        + "instead of writing a jq/python format-conversion snippet.")
                .inputSchema(Map.of(
                        "type", "object",
                        "properties", Map.of(
                                "input", Map.of("type", "string", "description", "The data to convert"),
                                "from", Map.of("type", "string", "description", "json, yaml, or csv"),
                                "to", Map.of("type", "string", "description", "json, yaml, or csv")),
                        "required", List.of("input", "from", "to")))
                .build();
        return toolSpec(tool, args -> {
            Map<String, String> body = Map.of(
                    "input", requireString(args, "input"),
                    "from", requireString(args, "from"),
                    "to", requireString(args, "to"));
            JsonNode result = client.postJson("/api/data/convert", body);
            return result.path("output").asText();
        });
    }

    private McpServerFeatures.SyncToolSpecification diffDataTool() {
        McpSchema.Tool tool = McpSchema.Tool.builder("diff_data")
                .description("Diff two texts (unified diff) or two structured documents (json/yaml, as a list "
                        + "of added/removed/changed paths), instead of writing a difflib/deepdiff script.")
                .inputSchema(Map.of(
                        "type", "object",
                        "properties", Map.of(
                                "left", Map.of("type", "string", "description", "The 'before' content"),
                                "right", Map.of("type", "string", "description", "The 'after' content"),
                                "format", Map.of("type", "string", "description", "text, json, or yaml")),
                        "required", List.of("left", "right", "format")))
                .build();
        return toolSpec(tool, args -> {
            Map<String, String> body = Map.of(
                    "left", requireString(args, "left"),
                    "right", requireString(args, "right"),
                    "format", requireString(args, "format"));
            JsonNode result = client.postJson("/api/data/diff", body);
            return result.path("diff").asText();
        });
    }

    private McpServerFeatures.SyncToolSpecification hashTextTool() {
        McpSchema.Tool tool = McpSchema.Tool.builder("hash_text")
                .description("Compute a hash (MD5/SHA-1/SHA-256/SHA-512) of a string.")
                .inputSchema(Map.of(
                        "type", "object",
                        "properties", Map.of(
                                "text", Map.of("type", "string"),
                                "algorithm", Map.of("type", "string", "description", "MD5, SHA-1, SHA-256 (default), or SHA-512")),
                        "required", List.of("text")))
                .build();
        return toolSpec(tool, args -> {
            Map<String, String> body = new LinkedHashMap<>();
            body.put("text", requireString(args, "text"));
            body.put("algorithm", (String) args.get("algorithm"));
            JsonNode result = client.postJson("/api/util/hash", body);
            return result.path("hash").asText();
        });
    }

    private McpServerFeatures.SyncToolSpecification base64Tool() {
        McpSchema.Tool tool = McpSchema.Tool.builder("base64")
                .description("Base64-encode or decode a string.")
                .inputSchema(Map.of(
                        "type", "object",
                        "properties", Map.of(
                                "text", Map.of("type", "string"),
                                "operation", Map.of("type", "string", "description", "encode or decode")),
                        "required", List.of("text", "operation")))
                .build();
        return toolSpec(tool, args -> {
            String text = requireString(args, "text");
            String path = switch (requireString(args, "operation").toLowerCase()) {
                case "encode" -> "/api/util/base64/encode";
                case "decode" -> "/api/util/base64/decode";
                default -> throw new ApiException("operation must be 'encode' or 'decode'");
            };
            JsonNode result = client.postJson(path, Map.of("text", text));
            return result.path("result").asText();
        });
    }

    private McpServerFeatures.SyncToolSpecification evaluateFormulaTool() {
        McpSchema.Tool tool = McpSchema.Tool.builder("evaluate_formula")
                .description("Evaluate a spreadsheet formula (e.g. \"=SUM(A1:A3)\") against a set of cell "
                        + "values, instead of writing/hand-computing it.")
                .inputSchema(Map.of(
                        "type", "object",
                        "properties", Map.of(
                                "cells", Map.of("type", "object",
                                        "description", "Cell reference (e.g. \"A1\") to its literal value"),
                                "formula", Map.of("type", "string", "description", "e.g. \"=A1+A2\" or \"=SUM(A1:A3)\"")),
                        "required", List.of("formula")))
                .build();
        return toolSpec(tool, args -> {
            @SuppressWarnings("unchecked")
            Map<String, Object> rawCells = (Map<String, Object>) args.getOrDefault("cells", Map.of());
            Map<String, String> cells = new LinkedHashMap<>();
            rawCells.forEach((k, v) -> cells.put(k, String.valueOf(v)));
            Map<String, Object> body = Map.of("cells", cells, "formula", requireString(args, "formula"));
            JsonNode result = client.postJson("/api/sheet/evaluate", body);
            return result.path("result").asText();
        });
    }

    // ---- shared plumbing ----

    @FunctionalInterface
    private interface ToolLogic {
        String run(Map<String, Object> arguments);
    }

    private McpServerFeatures.SyncToolSpecification toolSpec(McpSchema.Tool tool, ToolLogic logic) {
        return new McpServerFeatures.SyncToolSpecification(tool, (exchange, request) -> {
            long start = System.currentTimeMillis();
            String name = tool.name();
            log.info("tool call: {} args={}", name, summarizeArgs(request.arguments()));
            try {
                String result = logic.run(request.arguments());
                log.info("tool call ok: {} ({} ms)", name, System.currentTimeMillis() - start);
                return McpSchema.CallToolResult.builder().addTextContent(result).build();
            } catch (Exception e) {
                log.warn("tool call failed: {} ({} ms): {}", name, System.currentTimeMillis() - start, e.getMessage());
                return McpSchema.CallToolResult.builder().isError(true).addTextContent("Error: " + e.getMessage()).build();
            }
        });
    }

    /** Renders args for logging without dumping large values into the log. */
    private static String summarizeArgs(Map<String, Object> args) {
        if (args == null || args.isEmpty()) {
            return "{}";
        }
        StringBuilder sb = new StringBuilder("{");
        boolean first = true;
        for (Map.Entry<String, Object> entry : args.entrySet()) {
            if (!first) {
                sb.append(", ");
            }
            first = false;
            sb.append(entry.getKey()).append("=").append(summarizeValue(entry.getValue()));
        }
        return sb.append("}").toString();
    }

    private static String summarizeValue(Object value) {
        if (value instanceof String s) {
            return s.length() > 200 ? "<string, " + s.length() + " chars>" : "\"" + s + "\"";
        }
        if (value instanceof List<?> list) {
            return "<list, " + list.size() + " items>";
        }
        if (value instanceof Map<?, ?> map) {
            return "<object, " + map.size() + " keys>";
        }
        return String.valueOf(value);
    }

    private static String requireString(Map<String, Object> args, String key) {
        Object value = args.get(key);
        if (!(value instanceof String s) || s.isBlank()) {
            throw new ApiException(key + " must be a non-blank string");
        }
        return s;
    }
}
