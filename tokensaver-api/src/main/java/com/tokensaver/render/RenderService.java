package com.tokensaver.render;

import com.tokensaver.common.ApiException;
import org.commonmark.node.Node;
import org.commonmark.parser.Parser;
import org.commonmark.renderer.html.HtmlRenderer;
import org.springframework.stereotype.Service;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Locale;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;

/**
 * Renders Markdown or HTML to PDF or PNG by shelling out to the locally installed
 * wkhtmltopdf/wkhtmltoimage CLIs (deterministic, headless, no browser automation
 * framework needed) — the boilerplate an agent would otherwise reach for Puppeteer
 * or a similar headless-browser script to do.
 *
 * Requires "wkhtmltopdf" and/or "wkhtmltoimage" on PATH (e.g. `brew install
 * wkhtmltopdf` on macOS, or the packages of the same name on Debian/Ubuntu).
 */
@Service
public class RenderService {

    private static final long TIMEOUT_SECONDS = 60;
    private static final Parser MARKDOWN_PARSER = Parser.builder().build();
    private static final HtmlRenderer HTML_RENDERER = HtmlRenderer.builder().build();

    /**
     * @param content    the source document, either Markdown or HTML
     * @param sourceType "markdown" or "html"
     * @param format     "pdf" or "png"
     */
    public byte[] render(String content, String sourceType, String format) {
        if (content == null || content.isBlank()) {
            throw new ApiException("content must not be blank");
        }
        String html = switch (normalize(sourceType, "html")) {
            case "markdown", "md" -> toHtml(content);
            case "html" -> content;
            default -> throw new ApiException("sourceType must be 'markdown' or 'html'");
        };

        String targetFormat = normalize(format, "pdf");
        String binary = switch (targetFormat) {
            case "pdf" -> "wkhtmltopdf";
            case "png" -> "wkhtmltoimage";
            default -> throw new ApiException("format must be 'pdf' or 'png'");
        };

        return runRenderer(binary, html, targetFormat);
    }

    private String toHtml(String markdown) {
        Node document = MARKDOWN_PARSER.parse(markdown);
        return HTML_RENDERER.render(document);
    }

    private byte[] runRenderer(String binary, String html, String extension) {
        Path workDir;
        try {
            workDir = Files.createTempDirectory("tokensaver-render-");
        } catch (IOException e) {
            throw new ApiException("Failed to create temp directory for rendering: " + e.getMessage(), e);
        }
        try {
            Path inputFile = workDir.resolve("input.html");
            Path outputFile = workDir.resolve("output." + extension);
            Files.writeString(inputFile, html, StandardCharsets.UTF_8);

            Process process;
            try {
                process = new ProcessBuilder(binary, "--quiet", inputFile.toString(), outputFile.toString())
                        .redirectErrorStream(false)
                        .start();
            } catch (IOException e) {
                throw new ApiException("Could not run '" + binary + "' — is it installed and on PATH? "
                        + "(e.g. `brew install wkhtmltopdf`): " + e.getMessage(), e);
            }

            CompletableFuture<String> stderr = CompletableFuture.supplyAsync(() -> readAll(process.getErrorStream()));
            CompletableFuture<Void> stdout = CompletableFuture.runAsync(() -> drain(process.getInputStream()));

            boolean finished;
            try {
                finished = process.waitFor(TIMEOUT_SECONDS, TimeUnit.SECONDS);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new ApiException("Rendering was interrupted", e);
            }
            stdout.join();
            if (!finished) {
                process.destroyForcibly();
                throw new ApiException(binary + " timed out after " + TIMEOUT_SECONDS + "s");
            }
            if (process.exitValue() != 0 || !Files.exists(outputFile)) {
                throw new ApiException(binary + " failed: " + stderr.join());
            }
            return Files.readAllBytes(outputFile);
        } catch (IOException e) {
            throw new ApiException("Rendering failed: " + e.getMessage(), e);
        } finally {
            deleteRecursively(workDir);
        }
    }

    private String readAll(InputStream in) {
        try {
            return new String(in.readAllBytes(), StandardCharsets.UTF_8);
        } catch (IOException e) {
            return "(failed to read error output: " + e.getMessage() + ")";
        }
    }

    private void drain(InputStream in) {
        try {
            in.readAllBytes();
        } catch (IOException ignored) {
            // best-effort
        }
    }

    private static String normalize(String value, String fallback) {
        String v = (value == null || value.isBlank()) ? fallback : value;
        return v.toLowerCase(Locale.ROOT);
    }

    private void deleteRecursively(Path dir) {
        try (var paths = Files.walk(dir)) {
            paths.sorted(java.util.Comparator.reverseOrder()).forEach(p -> {
                try {
                    Files.deleteIfExists(p);
                } catch (IOException ignored) {
                    // best-effort cleanup
                }
            });
        } catch (IOException ignored) {
            // best-effort cleanup
        }
    }
}
