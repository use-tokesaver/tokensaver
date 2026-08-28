package com.tokensaver.files;

import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import javax.imageio.ImageIO;
import java.awt.*;
import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.Set;

/**
 * Resizes/reformats images with plain java.awt + ImageIO —
 * no ML, no external process, the kind of thing agents usually shell out to PIL for.
 */
@Service
public class ImageConvertService {

    private static final Set<String> SUPPORTED_FORMATS = Set.of("png", "jpg", "jpeg", "gif", "bmp");

    public byte[] convert(MultipartFile file, String targetFormat, Integer width, Integer height) {
        if (file == null || file.isEmpty()) {
            throw new ApiException("file must not be empty");
        }
        byte[] content;
        try {
            content = file.getBytes();
        } catch (IOException e) {
            throw new ApiException("Failed to read image: " + e.getMessage(), e);
        }
        return convert(content, targetFormat, width, height);
    }

    /** Same conversion, for callers that already have the image's bytes rather than a MultipartFile. */
    public byte[] convert(byte[] content, String targetFormat, Integer width, Integer height) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        String format = targetFormat == null ? "png" : targetFormat.toLowerCase();
        if (!SUPPORTED_FORMATS.contains(format)) {
            throw new ApiException("Unsupported target format: " + format + " (supported: " + SUPPORTED_FORMATS + ")");
        }

        BufferedImage source;
        try {
            source = ImageIO.read(new java.io.ByteArrayInputStream(content));
        } catch (IOException e) {
            throw new ApiException("Failed to read image: " + e.getMessage(), e);
        }
        if (source == null) {
            throw new ApiException("Uploaded file is not a readable image");
        }

        BufferedImage result = source;
        if (width != null || height != null) {
            int targetW = width != null ? width : scaledDimension(width, height, source.getWidth(), source.getHeight(), true);
            int targetH = height != null ? height : scaledDimension(width, height, source.getWidth(), source.getHeight(), false);
            result = resize(source, targetW, targetH);
        }

        // JPEG has no alpha channel — flatten onto white background to avoid write failure.
        if ((format.equals("jpg") || format.equals("jpeg")) && result.getColorModel().hasAlpha()) {
            result = flatten(result);
        }

        try {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            if (!ImageIO.write(result, format, out)) {
                throw new ApiException("No writer available for format: " + format);
            }
            return out.toByteArray();
        } catch (IOException e) {
            throw new ApiException("Failed to encode image: " + e.getMessage(), e);
        }
    }

    private int scaledDimension(Integer width, Integer height, int origW, int origH, boolean solvingWidth) {
        if (width != null && height == null) {
            return solvingWidth ? width : Math.round((float) origH * width / origW);
        }
        if (height != null && width == null) {
            return solvingWidth ? Math.round((float) origW * height / origH) : height;
        }
        return solvingWidth ? origW : origH;
    }

    private BufferedImage resize(BufferedImage source, int width, int height) {
        BufferedImage resized = new BufferedImage(width, height, BufferedImage.TYPE_INT_ARGB);
        Graphics2D g = resized.createGraphics();
        g.setRenderingHint(RenderingHints.KEY_INTERPOLATION, RenderingHints.VALUE_INTERPOLATION_BILINEAR);
        g.drawImage(source, 0, 0, width, height, null);
        g.dispose();
        return resized;
    }

    private BufferedImage flatten(BufferedImage source) {
        BufferedImage flattened = new BufferedImage(source.getWidth(), source.getHeight(), BufferedImage.TYPE_INT_RGB);
        Graphics2D g = flattened.createGraphics();
        g.setColor(Color.WHITE);
        g.fillRect(0, 0, source.getWidth(), source.getHeight());
        g.drawImage(source, 0, 0, null);
        g.dispose();
        return flattened;
    }
}
