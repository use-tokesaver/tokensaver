package com.tokensaver.util;

import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.zip.ZipEntry;
import java.util.zip.ZipInputStream;
import java.util.zip.ZipOutputStream;

/**
 * Zips a batch of uploaded files / unzips an archive's entries —
 * avoids the agent writing a Python zipfile snippet for a one-off bundle/unbundle.
 */
@Service
public class ArchiveService {

    public byte[] zip(MultipartFile[] files) {
        if (files == null || files.length == 0) {
            throw new ApiException("At least one file must be provided");
        }
        Map<String, byte[]> namedContents = new LinkedHashMap<>();
        try {
            for (MultipartFile file : files) {
                String name = file.getOriginalFilename() != null ? file.getOriginalFilename() : "file";
                namedContents.put(name, file.getBytes());
            }
        } catch (IOException e) {
            throw new ApiException("Failed to read uploaded file: " + e.getMessage(), e);
        }
        return zip(namedContents);
    }

    /** Same zip-building, for callers that already have file bytes rather than MultipartFiles. */
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

    public record ZipEntryResult(String name, long size, String textPreview) {
    }

    public List<ZipEntryResult> unzip(MultipartFile archive) {
        if (archive == null || archive.isEmpty()) {
            throw new ApiException("file must not be empty");
        }
        try {
            return unzip(archive.getBytes());
        } catch (IOException e) {
            throw new ApiException("Failed to read uploaded file: " + e.getMessage(), e);
        }
    }

    /** Same unzip logic, for callers that already have the archive's bytes rather than a MultipartFile. */
    public List<ZipEntryResult> unzip(byte[] content) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        List<ZipEntryResult> results = new ArrayList<>();
        try (ZipInputStream zis = new ZipInputStream(new ByteArrayInputStream(content))) {
            ZipEntry entry;
            while ((entry = zis.getNextEntry()) != null) {
                if (entry.isDirectory()) {
                    continue;
                }
                byte[] entryBytes = zis.readAllBytes();
                String preview = isLikelyText(entryBytes)
                        ? new String(entryBytes, StandardCharsets.UTF_8)
                        : "<binary content, " + entryBytes.length + " bytes>";
                results.add(new ZipEntryResult(entry.getName(), entryBytes.length, preview));
                zis.closeEntry();
            }
        } catch (IOException e) {
            throw new ApiException("Failed to read zip: " + e.getMessage(), e);
        }
        return results;
    }

    private boolean isLikelyText(byte[] content) {
        int sampleSize = Math.min(content.length, 512);
        for (int i = 0; i < sampleSize; i++) {
            byte b = content[i];
            if (b == 0) {
                return false;
            }
        }
        return true;
    }
}
