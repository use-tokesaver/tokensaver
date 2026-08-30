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
import org.springframework.boot.web.servlet.ServletRegistrationBean;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.util.ArrayList;
import java.util.Base64;
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
 * Because a remote MCP caller has no access to this server's filesystem, file-taking
 * tools exchange content inline as base64 rather than by local path. The server's
 * {@code instructions} (and each file tool's own description) steer an agent with shell
 * access toward calling curl directly for anything but small files, since base64-ing a
 * file into a tool call forces the model to emit the whole thing as output tokens.
 */
@Configuration
public class McpToolsConfiguration {

    private static final Logger log = LoggerFactory.getLogger("com.tokensaver.mcp");

    /** Hard cap on inline base64 file content. Above this, file tools reject the call outright. */
    private static final int MAX_INLINE_BYTES = 50_000;

    private static final String CURL_GUIDANCE = """
            IMPORTANT — read this before calling any file-based tool (extract_text, \
            convert_image, ocr, zip_files, unzip_file):

            Do NOT read a local file and pass its content as base64 to these tools. \
            That forces you to generate the entire file as output tokens just to make \
            the call — the exact cost this API exists to avoid. These tools enforce a \
            hard limit of %d bytes on inline content and will reject anything larger \
            with an error telling you to use curl instead.

            If you have shell/bash access, always call curl directly against the REST \
            endpoint instead of the MCP tool, regardless of file size:
              curl -F "file=@/path/to/file.pdf" <this-server-base-url>/api/files/extract-text
            Only use the MCP tool's inline-base64 form when there is genuinely no shell \
            available (e.g. a chat client with no code execution) and the file is small.

            For everything else (web_extract, hash_text, base64, convert_data, diff_data), \
            there's no file content involved — the MCP tool call is always the right choice.
            """.formatted(MAX_INLINE_BYTES);

    private final LoopbackApiClient client;

    public McpToolsConfiguration(LoopbackApiClient client) {
        this.client = client;
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
                .instructions(CURL_GUIDANCE)
                .capabilities(McpSchema.ServerCapabilities.builder().tools(true).build())
                .tools(
                        // text/URL tools — no file content involved, always call these directly
                        webExtractTool(),
                        convertDataTool(),
                        diffDataTool(),
                        hashTextTool(),
                        base64Tool(),
                        // file tools — base64 in/out; prefer curl directly for anything but small files
                        extractTextTool(),
                        convertImageTool(),
                        ocrTool(),
                        zipFilesTool(),
                        unzipFileTool())
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

    // ---- text/URL tools ----

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

    // ---- file tools (base64 in/out) ----

    private McpServerFeatures.SyncToolSpecification extractTextTool() {
        McpSchema.Tool tool = fileTool("extract_text",
                "Extract plain text from a .pdf/.docx/.xlsx/.txt/.md/.csv file, "
                        + "instead of writing a PyPDF2/python-docx/openpyxl script.",
                true);
        return toolSpec(tool, args -> {
            JsonNode result = client.postMultipartForJson(
                    "/api/files/extract-text", null, List.of(filePart(args, "file.txt")));
            return result.path("text").asText();
        });
    }

    private McpServerFeatures.SyncToolSpecification convertImageTool() {
        McpSchema.Tool tool = McpSchema.Tool.builder("convert_image")
                .description(withCurlHint("Resize/reformat an image, instead of writing a Pillow/PIL script. "
                        + "Pass the source image's raw bytes as base64; returns the converted image's bytes as base64."))
                .inputSchema(Map.of(
                        "type", "object",
                        "properties", Map.of(
                                "contentBase64", Map.of("type", "string", "description", "The source image's raw bytes, base64-encoded"),
                                "format", Map.of("type", "string", "description", "png, jpg, jpeg, gif, or bmp (default png)"),
                                "width", Map.of("type", "integer", "description", "Target width in pixels (optional)"),
                                "height", Map.of("type", "integer", "description", "Target height in pixels (optional)")),
                        "required", List.of("contentBase64")))
                .build();
        return toolSpec(tool, args -> {
            Map<String, String> query = new LinkedHashMap<>();
            query.put("format", (String) args.getOrDefault("format", "png"));
            if (args.get("width") != null) {
                query.put("width", String.valueOf(toInteger(args.get("width"))));
            }
            if (args.get("height") != null) {
                query.put("height", String.valueOf(toInteger(args.get("height"))));
            }
            byte[] converted = client.postMultipartForBytes("/api/files/image/convert",
                    LoopbackApiClient.buildQuery(query), List.of(filePart(args, "image.png")));
            return Base64.getEncoder().encodeToString(converted);
        });
    }

    private McpServerFeatures.SyncToolSpecification ocrTool() {
        McpSchema.Tool tool = fileTool("ocr",
                "Recognize text in an image or scanned PDF via a locally-run OCR engine, "
                        + "instead of writing a pytesseract script.",
                true);
        return toolSpec(tool, args -> {
            JsonNode result = client.postMultipartForJson("/api/files/ocr", null, List.of(filePart(args, "image.png")));
            return result.path("text").asText();
        });
    }

    private McpServerFeatures.SyncToolSpecification zipFilesTool() {
        McpSchema.Tool tool = McpSchema.Tool.builder("zip_files")
                .description(withCurlHint("Bundle files into a zip archive, instead of writing a Python zipfile script. "
                        + "Pass each file's raw bytes as base64; returns the zip's bytes as base64."))
                .inputSchema(Map.of(
                        "type", "object",
                        "properties", Map.of(
                                "files", Map.of("type", "array", "items", Map.of(
                                        "type", "object",
                                        "properties", Map.of(
                                                "filename", Map.of("type", "string"),
                                                "contentBase64", Map.of("type", "string")),
                                        "required", List.of("filename", "contentBase64")),
                                        "description", "Files to include")),
                        "required", List.of("files")))
                .build();
        return toolSpec(tool, args -> {
            @SuppressWarnings("unchecked")
            List<Map<String, Object>> files = (List<Map<String, Object>>) args.get("files");
            if (files == null || files.isEmpty()) {
                throw new ApiException("files must not be empty");
            }
            List<LoopbackApiClient.MultipartPart> parts = new ArrayList<>();
            for (Map<String, Object> file : files) {
                parts.add(new LoopbackApiClient.MultipartPart(
                        "files", requireString(file, "filename"), decodeBase64Checked(requireString(file, "contentBase64"))));
            }
            byte[] zipped = client.postMultipartForBytes("/api/util/zip", null, parts);
            return Base64.getEncoder().encodeToString(zipped);
        });
    }

    private McpServerFeatures.SyncToolSpecification unzipFileTool() {
        McpSchema.Tool tool = fileTool("unzip_file",
                "List a zip archive's entries (name, size, text preview), instead of writing a Python zipfile script.",
                false);
        return toolSpec(tool, args -> {
            JsonNode entries = client.postMultipartForJson(
                    "/api/util/unzip", null, List.of(filePart(args, "archive.zip")));
            StringBuilder sb = new StringBuilder();
            for (JsonNode entry : entries) {
                sb.append(entry.path("name").asText()).append(" (").append(entry.path("size").asLong()).append(" bytes)\n");
                sb.append(entry.path("textPreview").asText()).append("\n---\n");
            }
            return sb.toString();
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

    /** Renders args for logging without dumping large values (e.g. base64 file content) into the log. */
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

    /** Builds a single-file tool's schema: contentBase64 always required, filename optional unless {@code filenameRequired}. */
    private McpSchema.Tool fileTool(String name, String description, boolean filenameRequired) {
        Map<String, Object> properties = new LinkedHashMap<>();
        properties.put("contentBase64", Map.of("type", "string", "description", "The file's raw bytes, base64-encoded"));
        List<String> required = new ArrayList<>(List.of("contentBase64"));
        if (filenameRequired) {
            properties.put("filename", Map.of("type", "string", "description", "File name, used to detect the format (e.g. report.pdf)"));
            required.add("filename");
        }
        return McpSchema.Tool.builder(name)
                .description(withCurlHint(description))
                .inputSchema(Map.of("type", "object", "properties", properties, "required", required))
                .build();
    }

    private String withCurlHint(String description) {
        return "PREFER curl over this tool if you have shell access (see this server's MCP instructions) "
                + "— content over " + MAX_INLINE_BYTES + " bytes is rejected. " + description;
    }

    /** Reads {@code contentBase64} (required) and {@code filename} (optional, falling back to {@code defaultFilename}). */
    private LoopbackApiClient.MultipartPart filePart(Map<String, Object> args, String defaultFilename) {
        String filename = args.get("filename") instanceof String s && !s.isBlank() ? s : defaultFilename;
        return new LoopbackApiClient.MultipartPart("file", filename, decodeBase64Checked(requireString(args, "contentBase64")));
    }

    private static String requireString(Map<String, Object> args, String key) {
        Object value = args.get(key);
        if (!(value instanceof String s) || s.isBlank()) {
            throw new ApiException(key + " must be a non-blank string");
        }
        return s;
    }

    private static byte[] decodeBase64(String value) {
        try {
            return Base64.getDecoder().decode(value);
        } catch (IllegalArgumentException e) {
            throw new ApiException("contentBase64 is not valid base64: " + e.getMessage(), e);
        }
    }

    /** Decodes base64 content and rejects it outright if it's over {@link #MAX_INLINE_BYTES} — see this
     * tool's isError message for why: passing large files inline burns output tokens generating the
     * base64 in the first place, so the fix is to use curl, not a bigger limit. */
    private static byte[] decodeBase64Checked(String value) {
        byte[] content = decodeBase64(value);
        if (content.length > MAX_INLINE_BYTES) {
            throw new ApiException(String.format(
                    "This content is %,d bytes — over the %,d byte limit for inline base64 tool calls. "
                            + "If you have shell/bash access, run curl directly against this server's matching "
                            + "REST endpoint instead (e.g. curl -F \"file=@/path/to/file\" <base-url>/api/files/...) "
                            + "rather than reading the file and re-encoding it yourself.",
                    content.length, MAX_INLINE_BYTES));
        }
        return content;
    }

    private static Integer toInteger(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof Number n) {
            return n.intValue();
        }
        return Integer.parseInt(value.toString());
    }
}
