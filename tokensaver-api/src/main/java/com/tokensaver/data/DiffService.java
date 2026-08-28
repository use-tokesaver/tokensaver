package com.tokensaver.data;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLMapper;
import com.github.difflib.DiffUtils;
import com.github.difflib.UnifiedDiffUtils;
import com.github.difflib.patch.Patch;
import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeSet;

/**
 * Diffs two texts (unified diff) or two structured documents (JSON/YAML, as a list of
 * added/removed/changed paths) — the comparison logic an agent would otherwise write a
 * difflib/deepdiff script for.
 */
@Service
public class DiffService {

    private final ObjectMapper json = new ObjectMapper();
    private final YAMLMapper yaml = new YAMLMapper();

    public String diff(String left, String right, String format) {
        if (left == null || right == null) {
            throw new ApiException("left and right must not be null");
        }
        Format f = Format.parse(format);
        return switch (f) {
            case TEXT -> unifiedTextDiff(left, right);
            case JSON -> structuredDiff(parseTree(left, json, "json"), parseTree(right, json, "json"));
            case YAML -> structuredDiff(parseTree(left, yaml, "yaml"), parseTree(right, yaml, "yaml"));
        };
    }

    private JsonNode parseTree(String input, ObjectMapper mapper, String formatName) {
        try {
            return mapper.readTree(input);
        } catch (Exception e) {
            throw new ApiException("Failed to parse input as " + formatName + ": " + e.getMessage(), e);
        }
    }

    private String unifiedTextDiff(String left, String right) {
        List<String> leftLines = List.of(left.split("\n", -1));
        List<String> rightLines = List.of(right.split("\n", -1));
        Patch<String> patch = DiffUtils.diff(leftLines, rightLines);
        if (patch.getDeltas().isEmpty()) {
            return "(no differences)";
        }
        List<String> unified = UnifiedDiffUtils.generateUnifiedDiff("left", "right", leftLines, patch, 3);
        return String.join("\n", unified);
    }

    private String structuredDiff(JsonNode left, JsonNode right) {
        List<Map<String, Object>> changes = new ArrayList<>();
        compare("", left, right, changes);
        try {
            if (changes.isEmpty()) {
                return "(no differences)";
            }
            return json.writerWithDefaultPrettyPrinter().writeValueAsString(changes);
        } catch (Exception e) {
            throw new ApiException("Failed to serialize diff: " + e.getMessage(), e);
        }
    }

    private void compare(String path, JsonNode left, JsonNode right, List<Map<String, Object>> changes) {
        boolean leftMissing = left == null || left.isMissingNode();
        boolean rightMissing = right == null || right.isMissingNode();
        if (leftMissing && rightMissing) {
            return;
        }
        if (leftMissing) {
            changes.add(change(path, "added", null, right));
            return;
        }
        if (rightMissing) {
            changes.add(change(path, "removed", left, null));
            return;
        }
        if (left.equals(right)) {
            return;
        }
        if (left.isObject() && right.isObject()) {
            TreeSet<String> keys = new TreeSet<>();
            left.fieldNames().forEachRemaining(keys::add);
            right.fieldNames().forEachRemaining(keys::add);
            for (String key : keys) {
                compare(path.isEmpty() ? key : path + "." + key, left.path(key), right.path(key), changes);
            }
            return;
        }
        if (left.isArray() && right.isArray()) {
            int max = Math.max(left.size(), right.size());
            for (int i = 0; i < max; i++) {
                JsonNode l = i < left.size() ? left.get(i) : left.path(i);
                JsonNode r = i < right.size() ? right.get(i) : right.path(i);
                compare(path + "[" + i + "]", l, r, changes);
            }
            return;
        }
        changes.add(change(path, "changed", left, right));
    }

    private Map<String, Object> change(String path, String type, JsonNode before, JsonNode after) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("path", path.isEmpty() ? "$" : path);
        m.put("type", type);
        m.put("before", before);
        m.put("after", after);
        return m;
    }

    private enum Format {
        TEXT, JSON, YAML;

        static Format parse(String value) {
            if (value == null) {
                throw new ApiException("format must be one of text, json, yaml");
            }
            try {
                return Format.valueOf(value.trim().toUpperCase());
            } catch (IllegalArgumentException e) {
                throw new ApiException("Unsupported format: " + value + " (supported: text, json, yaml)");
            }
        }
    }
}
