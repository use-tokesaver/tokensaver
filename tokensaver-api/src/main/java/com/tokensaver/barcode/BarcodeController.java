package com.tokensaver.barcode;

import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;

import java.util.Map;

@RestController
@RequestMapping("/api/barcode")
public class BarcodeController {

    private final BarcodeService barcodeService;

    public BarcodeController(BarcodeService barcodeService) {
        this.barcodeService = barcodeService;
    }

    public record GenerateRequest(String text, String format, Integer width, Integer height) {
    }

    /**
     * POST /api/barcode/generate  { "text": "...", "format": "QR_CODE|CODE_128|EAN_13|...", "width", "height" }
     * Returns a PNG of the generated code, instead of the agent writing a python-qrcode script.
     */
    @PostMapping(value = "/generate", produces = MediaType.IMAGE_PNG_VALUE)
    public ResponseEntity<byte[]> generate(@RequestBody GenerateRequest request) {
        byte[] png = barcodeService.generate(request.text(), request.format(), request.width(), request.height());
        return ResponseEntity.ok()
                .header(HttpHeaders.CONTENT_DISPOSITION, "inline; filename=\"barcode.png\"")
                .contentType(MediaType.IMAGE_PNG)
                .body(png);
    }

    /**
     * POST /api/barcode/decode  (multipart "file": an image containing a QR/barcode)
     * Returns { "text": "...", "format": "..." }.
     */
    @PostMapping("/decode")
    public Map<String, String> decode(@RequestParam("file") MultipartFile file) {
        BarcodeService.DecodeResult result = barcodeService.decode(file);
        return Map.of("text", result.text(), "format", result.format());
    }
}
