package com.tokensaver.files;

import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.multipart.MultipartFile;

import java.util.Map;

@RestController
@RequestMapping("/api/files")
public class FileController {

    private final TextExtractService textExtractService;
    private final ImageConvertService imageConvertService;
    private final OcrService ocrService;

    public FileController(TextExtractService textExtractService, ImageConvertService imageConvertService,
            OcrService ocrService) {
        this.textExtractService = textExtractService;
        this.imageConvertService = imageConvertService;
        this.ocrService = ocrService;
    }

    /**
     * POST /api/files/extract-text  (multipart "file": .pdf/.docx/.xlsx/.txt/.md/.csv)
     * Returns the document's plain text, instead of the agent writing a
     * PyPDF2/python-docx/openpyxl script to do it.
     */
    @PostMapping("/extract-text")
    public Map<String, String> extractText(@RequestParam("file") MultipartFile file) {
        return Map.of("text", textExtractService.extract(file));
    }

    /**
     * POST /api/files/image/convert  (multipart "file", query params: format, width, height)
     * Resizes/reformats an image and returns the raw bytes.
     */
    @PostMapping(value = "/image/convert", produces = MediaType.APPLICATION_OCTET_STREAM_VALUE)
    public ResponseEntity<byte[]> convertImage(
            @RequestParam("file") MultipartFile file,
            @RequestParam(value = "format", required = false, defaultValue = "png") String format,
            @RequestParam(value = "width", required = false) Integer width,
            @RequestParam(value = "height", required = false) Integer height) {
        byte[] converted = imageConvertService.convert(file, format, width, height);
        return ResponseEntity.ok()
                .header(HttpHeaders.CONTENT_DISPOSITION, "inline; filename=\"converted." + format + "\"")
                .contentType(MediaType.parseMediaType("image/" + format))
                .body(converted);
    }

    /**
     * POST /api/files/ocr  (multipart "file": an image, or a scanned .pdf)
     * Returns recognized text via the locally installed Tesseract CLI, instead of the
     * agent writing a pytesseract script.
     */
    @PostMapping("/ocr")
    public Map<String, String> ocr(@RequestParam("file") MultipartFile file) {
        return Map.of("text", ocrService.ocr(file));
    }
}
