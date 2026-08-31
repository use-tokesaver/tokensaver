package com.tokensaver.util;

import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.Map;
import java.util.zip.ZipEntry;
import java.util.zip.ZipOutputStream;

/**
 * Bundles named byte contents into a zip archive. Not exposed as its own REST
 * endpoint — plain zip/unzip is trivial to do locally (`zip`/`unzip` on any machine)
 * and isn't worth a network round-trip. Kept here purely as the internal helper
 * {@code files/PdfController} uses to bundle a PDF split's output chunks.
 */
@Service
public class ArchiveService {

    public byte[] zip(Map<String, byte[]> namedContents) {
        if (namedContents == null || namedContents.isEmpty()) {
            throw new ApiException("At least one file must be provided");
        }
        ByteArrayOutputStream baos = new ByteArrayOutputStream();
        try (ZipOutputStream zos = new ZipOutputStream(baos)) {
            for (Map.Entry<String, byte[]> entry : namedContents.entrySet()) {
                zos.putNextEntry(new ZipEntry(entry.getKey()));
                zos.write(entry.getValue());
                zos.closeEntry();
            }
        } catch (IOException e) {
            throw new ApiException("Failed to build zip: " + e.getMessage(), e);
        }
        return baos.toByteArray();
    }
}
