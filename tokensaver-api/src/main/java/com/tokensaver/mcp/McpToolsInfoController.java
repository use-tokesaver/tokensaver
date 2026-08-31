package com.tokensaver.mcp;

import io.modelcontextprotocol.server.McpSyncServer;
import io.modelcontextprotocol.spec.McpSchema;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Plain GET endpoint listing the MCP tools, for humans/scripts to browse without
 * speaking the MCP protocol's JSON-RPC framing and session headers — unlike /mcp
 * itself, this is safe to open directly in a browser or hit with a bare curl.
 *
 * Reads the live tool list straight off {@link McpSyncServer}, so it can't drift out
 * of sync with what /mcp actually serves. Also documents file-based operations inline,
 * since those aren't MCP tools (see McpToolsConfiguration) and a model reading only
 * this JSON — without also seeing the MCP protocol's separate "instructions" field —
 * would otherwise have no way to know they exist or how to call them.
 */
@RestController
public class McpToolsInfoController {

    private static final List<Map<String, String>> FILE_OPERATIONS = List.of(
            fileOp("extract_text", "POST", "/api/files/extract-text",
                    "Extract plain text from a .pdf/.docx/.xlsx/.txt/.md/.csv file",
                    "curl -F \"file=@/path/to/file.pdf\" <base-url>/api/files/extract-text"),
            fileOp("convert_image", "POST", "/api/files/image/convert",
                    "Resize/reformat an image (query params: format, width, height)",
                    "curl -F \"file=@image.png\" \"<base-url>/api/files/image/convert?format=jpg&width=200\" -o out.jpg"),
            fileOp("ocr", "POST", "/api/files/ocr",
                    "Recognize text in an image or scanned PDF",
                    "curl -F \"file=@scan.png\" <base-url>/api/files/ocr"),
            fileOp("zip_files", "POST", "/api/util/zip",
                    "Bundle files into a zip archive (repeat -F files=@... per file)",
                    "curl -F \"files=@a.txt\" -F \"files=@b.txt\" <base-url>/api/util/zip -o bundle.zip"),
            fileOp("unzip_file", "POST", "/api/util/unzip",
                    "List a zip archive's entries (name, size, text preview)",
                    "curl -F \"file=@bundle.zip\" <base-url>/api/util/unzip"),
            fileOp("transcribe_audio", "POST", "/api/audio/transcribe",
                    "Transcribe an audio file via the locally installed Whisper CLI",
                    "curl -F \"file=@audio.mp3\" \"<base-url>/api/audio/transcribe?model=base\""),
            fileOp("render_html", "POST", "/api/render/html",
                    "Render Markdown or HTML to PDF or PNG (headless, via wkhtmltopdf/wkhtmltoimage)",
                    "curl -X POST <base-url>/api/render/html -H \"Content-Type: application/json\" "
                            + "-d '{\"content\":\"# Hi\",\"sourceType\":\"markdown\",\"format\":\"pdf\"}' -o out.pdf"),
            fileOp("generate_barcode", "POST", "/api/barcode/generate",
                    "Generate a QR code or linear barcode as a PNG",
                    "curl -X POST <base-url>/api/barcode/generate -H \"Content-Type: application/json\" "
                            + "-d '{\"text\":\"hello\",\"format\":\"QR_CODE\"}' -o code.png"),
            fileOp("decode_barcode", "POST", "/api/barcode/decode",
                    "Decode a QR code or barcode from an image",
                    "curl -F \"file=@code.png\" <base-url>/api/barcode/decode"),
            fileOp("merge_pdfs", "POST", "/api/pdf/merge",
                    "Merge two or more PDFs into one (repeat -F files=@... per file)",
                    "curl -F \"files=@a.pdf\" -F \"files=@b.pdf\" <base-url>/api/pdf/merge -o merged.pdf"),
            fileOp("split_pdf", "POST", "/api/pdf/split",
                    "Split a PDF into chunks of N pages, returned as a zip",
                    "curl -F \"file=@doc.pdf\" \"<base-url>/api/pdf/split?pagesPerFile=1\" -o split.zip"),
            fileOp("rotate_pdf", "POST", "/api/pdf/rotate",
                    "Rotate every page of a PDF by a multiple of 90 degrees",
                    "curl -F \"file=@doc.pdf\" \"<base-url>/api/pdf/rotate?degrees=90\" -o rotated.pdf"),
            fileOp("watermark_pdf", "POST", "/api/pdf/watermark",
                    "Stamp a diagonal text watermark on every page of a PDF",
                    "curl -F \"file=@doc.pdf\" \"<base-url>/api/pdf/watermark?text=DRAFT\" -o watermarked.pdf"),
            fileOp("fill_pdf_form", "POST", "/api/pdf/fill-form",
                    "Fill a PDF's AcroForm fields",
                    "curl -F \"file=@form.pdf\" -F 'fields={\"name\":\"John\"}' <base-url>/api/pdf/fill-form -o filled.pdf"));

    private final McpSyncServer mcpSyncServer;

    public McpToolsInfoController(McpSyncServer mcpSyncServer) {
        this.mcpSyncServer = mcpSyncServer;
    }

    /**
     * GET /api/mcp-tools ->
     * { "tools": [ { name, description, inputSchema }, ... ],
     *   "fileOperations": { "note": "...", "endpoints": [ { name, method, path, description, curl }, ... ] } }
     */
    @GetMapping("/api/mcp-tools")
    public Map<String, Object> listTools() {
        List<Map<String, Object>> tools = mcpSyncServer.listTools().stream()
                .map(this::toSummary)
                .toList();

        Map<String, Object> fileOperations = new LinkedHashMap<>();
        fileOperations.put("note", "These are NOT MCP tools — calling a tool with file content inline as base64 "
                + "forces a model to generate the whole file as output tokens. Call these REST endpoints "
                + "directly with curl instead (swap <base-url> for this server's actual address).");
        fileOperations.put("endpoints", FILE_OPERATIONS);

        Map<String, Object> result = new LinkedHashMap<>();
        result.put("tools", tools);
        result.put("fileOperations", fileOperations);
        return result;
    }

    private Map<String, Object> toSummary(McpSchema.Tool tool) {
        Map<String, Object> summary = new LinkedHashMap<>();
        summary.put("name", tool.name());
        summary.put("description", tool.description());
        summary.put("inputSchema", tool.inputSchema());
        return summary;
    }

    private static Map<String, String> fileOp(String name, String method, String path, String description, String curl) {
        Map<String, String> op = new LinkedHashMap<>();
        op.put("name", name);
        op.put("method", method);
        op.put("path", path);
        op.put("description", description);
        op.put("curl", curl);
        return op;
    }
}
