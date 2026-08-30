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
 * of sync with what /mcp actually serves.
 */
@RestController
public class McpToolsInfoController {

    private final McpSyncServer mcpSyncServer;

    public McpToolsInfoController(McpSyncServer mcpSyncServer) {
        this.mcpSyncServer = mcpSyncServer;
    }

    /** GET /api/mcp-tools -> { "tools": [ { name, description, inputSchema }, ... ] } */
    @GetMapping("/api/mcp-tools")
    public Map<String, Object> listTools() {
        List<Map<String, Object>> tools = mcpSyncServer.listTools().stream()
                .map(this::toSummary)
                .toList();
        return Map.of("tools", tools);
    }

    private Map<String, Object> toSummary(McpSchema.Tool tool) {
        Map<String, Object> summary = new LinkedHashMap<>();
        summary.put("name", tool.name());
        summary.put("description", tool.description());
        summary.put("inputSchema", tool.inputSchema());
        return summary;
    }
}
