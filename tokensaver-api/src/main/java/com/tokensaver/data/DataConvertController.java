package com.tokensaver.data;

import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

import java.util.Map;

@RestController
public class DataConvertController {

    private final DataConvertService service;
    private final DiffService diffService;

    public DataConvertController(DataConvertService service, DiffService diffService) {
        this.service = service;
        this.diffService = diffService;
    }

    public record ConvertRequest(String input, String from, String to) {
    }

    /**
     * POST /api/data/convert  { "input": "...", "from": "json|yaml|csv", "to": "json|yaml|csv" }
     * Replaces the one-off jq/python snippet agents write for format juggling.
     */
    @PostMapping("/api/data/convert")
    public Map<String, String> convert(@RequestBody ConvertRequest request) {
        return Map.of("output", service.convert(request.input(), request.from(), request.to()));
    }

    public record DiffRequest(String left, String right, String format) {
    }

    /**
     * POST /api/data/diff  { "left": "...", "right": "...", "format": "text|json|yaml" }
     * Returns a unified diff (text) or a list of added/removed/changed paths (json/yaml),
     * instead of the agent writing a difflib/deepdiff script.
     */
    @PostMapping("/api/data/diff")
    public Map<String, String> diff(@RequestBody DiffRequest request) {
        return Map.of("diff", diffService.diff(request.left(), request.right(), request.format()));
    }
}
