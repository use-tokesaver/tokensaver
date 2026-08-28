package com.tokensaver.files;

import com.tokensaver.common.ApiException;
import org.apache.pdfbox.Loader;
import org.apache.pdfbox.pdmodel.PDDocument;
import org.apache.pdfbox.rendering.PDFRenderer;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Locale;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;

/**
 * OCRs images and scanned PDFs by shelling out to the locally installed Tesseract CLI
 * (a deterministic, locally-run engine — not a paid/LLM API) — the boilerplate an agent
 * would otherwise reach for pytesseract to do.
 *
 * Requires the "tesseract" binary on PATH (e.g. `brew install tesseract` on macOS,
 * `apt install tesseract-ocr` on Debian/Ubuntu).
 */
@Service
public class OcrService {

    private static final int RENDER_DPI = 200;
    private static final long TIMEOUT_SECONDS = 60;

    public String ocr(MultipartFile file) {
        if (file == null || file.isEmpty()) {
            throw new ApiException("file must not be empty");
        }
        String name = file.getOriginalFilename() == null ? "" : file.getOriginalFilename();
        try (InputStream in = file.getInputStream()) {
            return ocr(in.readAllBytes(), name);
        } catch (IOException e) {
            throw new ApiException("Failed to read uploaded file: " + e.getMessage(), e);
        }
    }

    /** Same OCR, for callers that already have the file's bytes rather than a MultipartFile. */
    public String ocr(byte[] content, String filename) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        String name = (filename == null ? "" : filename).toLowerCase(Locale.ROOT);
        if (name.endsWith(".pdf")) {
            return ocrPdf(content);
        }
        return ocrImageBytes(content);
    }

    private String ocrPdf(byte[] content) {
        try (PDDocument document = Loader.loadPDF(content)) {
            PDFRenderer renderer = new PDFRenderer(document);
            StringBuilder sb = new StringBuilder();
            for (int page = 0; page < document.getNumberOfPages(); page++) {
                BufferedImage image = renderer.renderImageWithDPI(page, RENDER_DPI);
                sb.append(runTesseractOnImage(image)).append("\n");
            }
            return sb.toString();
        } catch (IOException e) {
            throw new ApiException("Failed to render PDF for OCR: " + e.getMessage(), e);
        }
    }

    private String ocrImageBytes(byte[] content) {
        BufferedImage image;
        try {
            image = ImageIO.read(new ByteArrayInputStream(content));
        } catch (IOException e) {
            throw new ApiException("Failed to read image: " + e.getMessage(), e);
        }
        if (image == null) {
            throw new ApiException("Uploaded file is not a readable image or PDF");
        }
        return runTesseractOnImage(image);
    }

    private String runTesseractOnImage(BufferedImage image) {
        Path tempImage;
        try {
            tempImage = Files.createTempFile("tokensaver-ocr-", ".png");
        } catch (IOException e) {
            throw new ApiException("Failed to create temp file for OCR: " + e.getMessage(), e);
        }
        try {
            ImageIO.write(image, "png", tempImage.toFile());
            return runTesseract(tempImage);
        } catch (IOException e) {
            throw new ApiException("Failed to write temp image for OCR: " + e.getMessage(), e);
        } finally {
            try {
                Files.deleteIfExists(tempImage);
            } catch (IOException ignored) {
                // best-effort cleanup
            }
        }
    }

    private String runTesseract(Path imagePath) {
        Process process;
        try {
            process = new ProcessBuilder("tesseract", imagePath.toString(), "stdout")
                    .redirectErrorStream(false)
                    .start();
        } catch (IOException e) {
            throw new ApiException("Could not run 'tesseract' — is it installed and on PATH? "
                    + "(e.g. `brew install tesseract`): " + e.getMessage(), e);
        }

        CompletableFuture<String> stdout = CompletableFuture.supplyAsync(() -> readAll(process.getInputStream()));
        CompletableFuture<String> stderr = CompletableFuture.supplyAsync(() -> readAll(process.getErrorStream()));

        boolean finished;
        try {
            finished = process.waitFor(TIMEOUT_SECONDS, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new ApiException("OCR was interrupted", e);
        }
        if (!finished) {
            process.destroyForcibly();
            throw new ApiException("tesseract timed out after " + TIMEOUT_SECONDS + "s");
        }
        if (process.exitValue() != 0) {
            throw new ApiException("tesseract failed: " + stderr.join());
        }
        return stdout.join();
    }

    private String readAll(InputStream in) {
        try {
            return new String(in.readAllBytes(), StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw new ApiException("Failed to read tesseract output: " + e.getMessage(), e);
        }
    }
}
