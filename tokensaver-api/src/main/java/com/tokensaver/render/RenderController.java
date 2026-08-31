package com.tokensaver.render;

import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/render")
public class RenderController {

    private final RenderService renderService;

    public RenderController(RenderService renderService) {
        this.renderService = renderService;
    }

    public record RenderRequest(String content, String sourceType, String format) {
    }

    /**
     * POST /api/render/html  { "content": "...", "sourceType": "markdown|html", "format": "pdf|png" }
     * Renders Markdown or HTML headlessly and returns the resulting file's bytes,
     * instead of the agent scripting a headless-browser (Puppeteer/Playwright) render.
     */
    @PostMapping(value = "/html", produces = MediaType.APPLICATION_OCTET_STREAM_VALUE)
    public ResponseEntity<byte[]> render(@RequestBody RenderRequest request) {
        String format = request.format() == null || request.format().isBlank() ? "pdf" : request.format();
        byte[] bytes = renderService.render(request.content(), request.sourceType(), format);
        MediaType mediaType = "png".equalsIgnoreCase(format) ? MediaType.IMAGE_PNG : MediaType.APPLICATION_PDF;
        return ResponseEntity.ok()
                .header(HttpHeaders.CONTENT_DISPOSITION, "inline; filename=\"rendered." + format.toLowerCase() + "\"")
                .contentType(mediaType)
                .body(bytes);
    }
}
