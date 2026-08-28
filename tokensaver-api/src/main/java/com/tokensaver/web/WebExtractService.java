package com.tokensaver.web;

import com.tokensaver.common.ApiException;
import org.jsoup.Jsoup;
import org.jsoup.nodes.Document;
import org.jsoup.nodes.Element;
import org.jsoup.select.Elements;
import org.springframework.stereotype.Service;

import java.io.IOException;
import java.net.InetAddress;
import java.net.URI;
import java.net.UnknownHostException;

/**
 * Fetches a URL and reduces it to readable content — the same boilerplate-stripping
 * work an agent would otherwise write a scraping script for.
 */
@Service
public class WebExtractService {

    private static final int TIMEOUT_MS = 15_000;
    private static final String USER_AGENT =
            "Mozilla/5.0 (compatible; TokenSaverBot/0.1; +poc)";

    public WebExtractResult extract(String url) {
        validateUrl(url);

        Document doc;
        try {
            doc = Jsoup.connect(url)
                    .userAgent(USER_AGENT)
                    .timeout(TIMEOUT_MS)
                    .followRedirects(true)
                    .get();
        } catch (IOException e) {
            throw new ApiException("Failed to fetch URL: " + e.getMessage(), e);
        }

        String title = doc.title();

        // Strip elements that are never the "content" of a page.
        doc.select("script, style, noscript, nav, footer, header, aside, form, iframe, svg, button").remove();
        doc.select("[role=navigation], [role=banner], [role=contentinfo]").remove();

        Element contentRoot = pickLargestTextBlock(doc);
        String text = contentRoot.text();
        String markdown = toMarkdown(contentRoot);

        return new WebExtractResult(url, title, text, markdown);
    }

    private void validateUrl(String url) {
        if (url == null || url.isBlank()) {
            throw new ApiException("url must not be blank");
        }
        URI uri;
        try {
            uri = URI.create(url);
        } catch (IllegalArgumentException e) {
            throw new ApiException("Malformed url: " + url);
        }
        String scheme = uri.getScheme();
        if (scheme == null || !(scheme.equalsIgnoreCase("http") || scheme.equalsIgnoreCase("https"))) {
            throw new ApiException("Only http/https URLs are supported");
        }
        if (uri.getHost() == null) {
            throw new ApiException("URL must include a host");
        }
        rejectPrivateOrLocalTargets(uri.getHost());
    }

    /** Basic SSRF guard: this endpoint takes attacker-influenced URLs, so refuse to fetch internal/loopback targets. */
    private void rejectPrivateOrLocalTargets(String host) {
        if (host.equalsIgnoreCase("localhost")) {
            throw new ApiException("Refusing to fetch localhost/internal targets");
        }
        InetAddress addr;
        try {
            addr = InetAddress.getByName(host);
        } catch (UnknownHostException e) {
            throw new ApiException("Could not resolve host: " + host);
        }
        if (addr.isLoopbackAddress() || addr.isAnyLocalAddress() || addr.isLinkLocalAddress()
                || addr.isSiteLocalAddress() || addr.isMulticastAddress()) {
            throw new ApiException("Refusing to fetch localhost/internal targets");
        }
    }

    /** Picks the element with the most direct text density — a cheap readability heuristic. */
    private Element pickLargestTextBlock(Document doc) {
        Elements candidates = doc.select("article, main, [role=main], div, section, body");
        Element best = doc.body() != null ? doc.body() : doc;
        int bestScore = -1;
        for (Element el : candidates) {
            int score = el.ownText().length() + (el.text().length() / 4);
            if (score > bestScore) {
                bestScore = score;
                best = el;
            }
        }
        return best;
    }

    private String toMarkdown(Element root) {
        StringBuilder sb = new StringBuilder();
        appendMarkdown(root, sb);
        return sb.toString().replaceAll("\n{3,}", "\n\n").trim();
    }

    private void appendMarkdown(Element el, StringBuilder sb) {
        for (var node : el.childNodes()) {
            if (node instanceof Element child) {
                switch (child.tagName()) {
                    case "h1" -> sb.append("\n# ").append(child.text()).append("\n");
                    case "h2" -> sb.append("\n## ").append(child.text()).append("\n");
                    case "h3" -> sb.append("\n### ").append(child.text()).append("\n");
                    case "h4", "h5", "h6" -> sb.append("\n#### ").append(child.text()).append("\n");
                    case "p" -> sb.append("\n").append(child.text()).append("\n");
                    case "li" -> sb.append("- ").append(child.text()).append("\n");
                    case "a" -> sb.append("[").append(child.text()).append("](")
                            .append(child.absUrl("href")).append(")");
                    case "br" -> sb.append("\n");
                    default -> appendMarkdown(child, sb);
                }
            }
        }
    }

    public record WebExtractResult(String url, String title, String text, String markdown) {
    }
}
