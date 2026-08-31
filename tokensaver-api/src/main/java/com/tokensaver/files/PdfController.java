package com.tokensaver.files;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.tokensaver.common.ApiException;
import com.tokensaver.util.ArchiveService;
import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

@RestController
@RequestMapping("/api/pdf")
public class PdfController {

    private final PdfManipulationService pdfService;
    private final ArchiveService archiveService;
    private final ObjectMapper mapper = new ObjectMapper();

    public PdfController(PdfManipulationService pdfService, ArchiveService archiveService) {
        this.pdfService = pdfService;
        this.archiveService = archiveService;
    }

    /**
     * POST /api/pdf/merge  (multipart "files", repeatable, at least 2)
     * Returns the merged PDF's bytes, instead of the agent writing a PyPDF2 merge script.
     */
    @PostMapping(value = "/merge", produces = MediaType.APPLICATION_PDF_VALUE)
    public ResponseEntity<byte[]> merge(@RequestParam("files") List<MultipartFile> files) {
        return pdfResponse(pdfService.merge(files), "merged.pdf");
    }

    /**
     * POST /api/pdf/split  (multipart "file", query param "pagesPerFile", default 1)
     * Returns a zip bundling each resulting PDF chunk.
     */
    @PostMapping(value = "/split", produces = "application/zip")
    public ResponseEntity<byte[]> split(
            @RequestParam("file") MultipartFile file,
            @RequestParam(value = "pagesPerFile", required = false, defaultValue = "1") int pagesPerFile) {
        byte[] content = readBytes(file);
        List<byte[]> parts = pdfService.split(content, pagesPerFile);
        Map<String, byte[]> named = new LinkedHashMap<>();
        for (int i = 0; i < parts.size(); i++) {
            named.put("part-" + (i + 1) + ".pdf", parts.get(i));
        }
        byte[] zip = archiveService.zip(named);
        return ResponseEntity.ok()
                .header(HttpHeaders.CONTENT_DISPOSITION, "attachment; filename=\"split.zip\"")
                .contentType(MediaType.parseMediaType("application/zip"))
                .body(zip);
    }

    /**
     * POST /api/pdf/rotate  (multipart "file", query param "degrees": multiple of 90)
     * Returns the rotated PDF's bytes.
     */
    @PostMapping(value = "/rotate", produces = MediaType.APPLICATION_PDF_VALUE)
    public ResponseEntity<byte[]> rotate(
            @RequestParam("file") MultipartFile file,
            @RequestParam("degrees") int degrees) {
        return pdfResponse(pdfService.rotate(readBytes(file), degrees), "rotated.pdf");
    }

    /**
     * POST /api/pdf/watermark  (multipart "file", query param "text")
     * Returns the watermarked PDF's bytes.
     */
    @PostMapping(value = "/watermark", produces = MediaType.APPLICATION_PDF_VALUE)
    public ResponseEntity<byte[]> watermark(
            @RequestParam("file") MultipartFile file,
            @RequestParam("text") String text) {
        return pdfResponse(pdfService.watermark(readBytes(file), text), "watermarked.pdf");
    }

    /**
     * POST /api/pdf/fill-form  (multipart "file", "fields": a JSON object string, e.g. {"name":"John"})
     * Returns the filled PDF's bytes.
     */
    @PostMapping(value = "/fill-form", produces = MediaType.APPLICATION_PDF_VALUE)
    public ResponseEntity<byte[]> fillForm(
            @RequestParam("file") MultipartFile file,
            @RequestParam("fields") String fieldsJson) {
        Map<String, String> fields;
        try {
            fields = mapper.readValue(fieldsJson, new TypeReference<Map<String, String>>() {
            });
        } catch (IOException e) {
            throw new ApiException("fields must be a JSON object of field name -> value: " + e.getMessage(), e);
        }
        return pdfResponse(pdfService.fillForm(readBytes(file), fields), "filled.pdf");
    }

    private byte[] readBytes(MultipartFile file) {
        if (file == null || file.isEmpty()) {
            throw new ApiException("file must not be empty");
        }
        try {
            return file.getBytes();
        } catch (IOException e) {
            throw new ApiException("Failed to read uploaded file: " + e.getMessage(), e);
        }
    }

    private ResponseEntity<byte[]> pdfResponse(byte[] content, String filename) {
        return ResponseEntity.ok()
                .header(HttpHeaders.CONTENT_DISPOSITION, "inline; filename=\"" + filename + "\"")
                .contentType(MediaType.APPLICATION_PDF)
                .body(content);
    }
}
