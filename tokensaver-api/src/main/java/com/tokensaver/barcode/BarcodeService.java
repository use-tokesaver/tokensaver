package com.tokensaver.barcode;

import com.google.zxing.BarcodeFormat;
import com.google.zxing.BinaryBitmap;
import com.google.zxing.LuminanceSource;
import com.google.zxing.MultiFormatReader;
import com.google.zxing.MultiFormatWriter;
import com.google.zxing.NotFoundException;
import com.google.zxing.Result;
import com.google.zxing.WriterException;
import com.google.zxing.client.j2se.BufferedImageLuminanceSource;
import com.google.zxing.client.j2se.MatrixToImageWriter;
import com.google.zxing.common.BitMatrix;
import com.google.zxing.common.HybridBinarizer;
import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.Locale;

/**
 * Generates and decodes QR codes and linear barcodes via ZXing (a plain Java library,
 * no LLM/AI involved) — the boilerplate an agent would otherwise reach for a
 * python-qrcode/zxing-cli script to do.
 */
@Service
public class BarcodeService {

    private static final int DEFAULT_SIZE = 300;

    public byte[] generate(String text, String format, Integer width, Integer height) {
        if (text == null || text.isBlank()) {
            throw new ApiException("text must not be blank");
        }
        BarcodeFormat barcodeFormat = parseFormat(format, BarcodeFormat.QR_CODE);
        int w = width != null && width > 0 ? width : DEFAULT_SIZE;
        int h = height != null && height > 0 ? height : DEFAULT_SIZE;
        try {
            BitMatrix matrix = new MultiFormatWriter().encode(text, barcodeFormat, w, h);
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            MatrixToImageWriter.writeToStream(matrix, "PNG", out);
            return out.toByteArray();
        } catch (WriterException | IOException e) {
            throw new ApiException("Failed to generate " + barcodeFormat + ": " + e.getMessage(), e);
        }
    }

    public record DecodeResult(String text, String format) {
    }

    public DecodeResult decode(MultipartFile file) {
        if (file == null || file.isEmpty()) {
            throw new ApiException("file must not be empty");
        }
        try {
            return decode(file.getBytes());
        } catch (IOException e) {
            throw new ApiException("Failed to read uploaded file: " + e.getMessage(), e);
        }
    }

    /** Same decoding, for callers that already have the image's bytes rather than a MultipartFile. */
    public DecodeResult decode(byte[] content) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        BufferedImage image;
        try {
            image = ImageIO.read(new ByteArrayInputStream(content));
        } catch (IOException e) {
            throw new ApiException("Failed to read image: " + e.getMessage(), e);
        }
        if (image == null) {
            throw new ApiException("Uploaded file is not a readable image");
        }
        LuminanceSource source = new BufferedImageLuminanceSource(image);
        BinaryBitmap bitmap = new BinaryBitmap(new HybridBinarizer(source));
        try {
            Result result = new MultiFormatReader().decode(bitmap);
            return new DecodeResult(result.getText(), result.getBarcodeFormat().name());
        } catch (NotFoundException e) {
            throw new ApiException("No barcode/QR code found in the image");
        }
    }

    private static BarcodeFormat parseFormat(String format, BarcodeFormat fallback) {
        if (format == null || format.isBlank()) {
            return fallback;
        }
        try {
            return BarcodeFormat.valueOf(format.toUpperCase(Locale.ROOT));
        } catch (IllegalArgumentException e) {
            throw new ApiException("Unsupported barcode format: " + format
                    + " (e.g. QR_CODE, CODE_128, EAN_13, UPC_A, PDF_417)");
        }
    }
}
