package com.tokensaver.util;

import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.multipart.MultipartFile;

import java.util.List;
import java.util.Map;

@RestController
@RequestMapping("/api/util")
public class UtilController {

    private final HashService hashService;
    private final Base64Service base64Service;
    private final ArchiveService archiveService;

    public UtilController(HashService hashService, Base64Service base64Service, ArchiveService archiveService) {
        this.hashService = hashService;
        this.base64Service = base64Service;
        this.archiveService = archiveService;
    }

    public record HashRequest(String text, String algorithm) {
    }

    /** POST /api/util/hash  { "text": "...", "algorithm": "SHA-256" } */
    @PostMapping("/hash")
    public Map<String, String> hash(@RequestBody HashRequest request) {
        return Map.of("hash", hashService.hash(request.text(), request.algorithm()));
    }

    public record Base64Request(String text) {
    }

    /** POST /api/util/base64/encode  { "text": "..." } */
    @PostMapping("/base64/encode")
    public Map<String, String> base64Encode(@RequestBody Base64Request request) {
        return Map.of("result", base64Service.encode(request.text()));
    }

    /** POST /api/util/base64/decode  { "text": "..." } */
    @PostMapping("/base64/decode")
    public Map<String, String> base64Decode(@RequestBody Base64Request request) {
        return Map.of("result", base64Service.decode(request.text()));
    }

    /** POST /api/util/zip  (multipart "files": one or more) -> application/zip bytes */
    @PostMapping(value = "/zip", produces = "application/zip")
    public ResponseEntity<byte[]> zip(@RequestParam("files") MultipartFile[] files) {
        byte[] zipped = archiveService.zip(files);
        return ResponseEntity.ok()
                .header(HttpHeaders.CONTENT_DISPOSITION, "attachment; filename=\"bundle.zip\"")
                .contentType(MediaType.valueOf("application/zip"))
                .body(zipped);
    }

    /** POST /api/util/unzip  (multipart "file": a .zip) -> entry name/size/text-preview list */
    @PostMapping("/unzip")
    public List<ArchiveService.ZipEntryResult> unzip(@RequestParam("file") MultipartFile file) {
        return archiveService.unzip(file);
    }
}
