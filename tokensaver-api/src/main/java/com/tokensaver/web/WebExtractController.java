package com.tokensaver.web;

import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class WebExtractController {

    private final WebExtractService service;

    public WebExtractController(WebExtractService service) {
        this.service = service;
    }

    public record ExtractRequest(String url) {
    }

    /**
     * POST /api/web/extract  { "url": "https://example.com/article" }
     * Fetches the page and returns boilerplate-stripped title/text/markdown,
     * instead of the agent writing+debugging a scrape-and-clean script.
     */
    @PostMapping("/api/web/extract")
    public WebExtractService.WebExtractResult extract(@RequestBody ExtractRequest request) {
        return service.extract(request.url());
    }
}
