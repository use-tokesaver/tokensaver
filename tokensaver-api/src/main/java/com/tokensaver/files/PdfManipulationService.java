package com.tokensaver.files;

import com.tokensaver.common.ApiException;
import org.apache.pdfbox.Loader;
import org.apache.pdfbox.io.RandomAccessReadBuffer;
import org.apache.pdfbox.multipdf.PDFMergerUtility;
import org.apache.pdfbox.multipdf.Splitter;
import org.apache.pdfbox.pdmodel.PDDocument;
import org.apache.pdfbox.pdmodel.PDPage;
import org.apache.pdfbox.pdmodel.PDPageContentStream;
import org.apache.pdfbox.pdmodel.font.PDFont;
import org.apache.pdfbox.pdmodel.font.Standard14Fonts;
import org.apache.pdfbox.pdmodel.font.PDType1Font;
import org.apache.pdfbox.pdmodel.interactive.form.PDAcroForm;
import org.apache.pdfbox.pdmodel.interactive.form.PDField;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * PDF manipulation beyond text extraction — merge, split, rotate, watermark, and
 * fill form fields — via PDFBox, instead of the agent writing a PyPDF2/pypdf script.
 */
@Service
public class PdfManipulationService {

    public byte[] merge(List<MultipartFile> files) {
        if (files == null || files.size() < 2) {
            throw new ApiException("At least two files must be provided to merge");
        }
        PDFMergerUtility merger = new PDFMergerUtility();
        try {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            for (MultipartFile file : files) {
                merger.addSource(new RandomAccessReadBuffer(file.getBytes()));
            }
            merger.setDestinationStream(out);
            merger.mergeDocuments(null);
            return out.toByteArray();
        } catch (IOException e) {
            throw new ApiException("Failed to merge PDFs: " + e.getMessage(), e);
        }
    }

    public List<byte[]> split(byte[] content, int pagesPerFile) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        if (pagesPerFile < 1) {
            throw new ApiException("pagesPerFile must be at least 1");
        }
        try (PDDocument document = Loader.loadPDF(content)) {
            Splitter splitter = new Splitter();
            splitter.setSplitAtPage(pagesPerFile);
            List<PDDocument> parts = splitter.split(document);
            List<byte[]> results = new ArrayList<>();
            for (PDDocument part : parts) {
                ByteArrayOutputStream out = new ByteArrayOutputStream();
                part.save(out);
                part.close();
                results.add(out.toByteArray());
            }
            return results;
        } catch (IOException e) {
            throw new ApiException("Failed to split PDF: " + e.getMessage(), e);
        }
    }

    public byte[] rotate(byte[] content, int degrees) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        int normalizedDegrees = ((degrees % 360) + 360) % 360;
        if (normalizedDegrees % 90 != 0) {
            throw new ApiException("degrees must be a multiple of 90");
        }
        try (PDDocument document = Loader.loadPDF(content)) {
            for (PDPage page : document.getPages()) {
                page.setRotation(page.getRotation() + normalizedDegrees);
            }
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            document.save(out);
            return out.toByteArray();
        } catch (IOException e) {
            throw new ApiException("Failed to rotate PDF: " + e.getMessage(), e);
        }
    }

    public byte[] watermark(byte[] content, String text) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        if (text == null || text.isBlank()) {
            throw new ApiException("text must not be blank");
        }
        try (PDDocument document = Loader.loadPDF(content)) {
            PDFont font = new PDType1Font(Standard14Fonts.FontName.HELVETICA_BOLD);
            for (PDPage page : document.getPages()) {
                var box = page.getMediaBox();
                float centerX = box.getWidth() / 2f;
                float centerY = box.getHeight() / 2f;
                try (PDPageContentStream contentStream = new PDPageContentStream(
                        document, page, PDPageContentStream.AppendMode.APPEND, true, true)) {
                    contentStream.setNonStrokingColor(new java.awt.Color(200, 200, 200));
                    contentStream.beginText();
                    contentStream.setFont(font, 48);
                    contentStream.setTextMatrix(
                            org.apache.pdfbox.util.Matrix.getRotateInstance(Math.toRadians(45), centerX - 150, centerY));
                    contentStream.showText(text);
                    contentStream.endText();
                }
            }
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            document.save(out);
            return out.toByteArray();
        } catch (IOException e) {
            throw new ApiException("Failed to watermark PDF: " + e.getMessage(), e);
        }
    }

    public byte[] fillForm(byte[] content, Map<String, String> fieldValues) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        try (PDDocument document = Loader.loadPDF(content)) {
            PDAcroForm form = document.getDocumentCatalog().getAcroForm();
            if (form == null) {
                throw new ApiException("PDF has no fillable form fields");
            }
            Map<String, String> notFilled = new LinkedHashMap<>();
            if (fieldValues != null) {
                for (Map.Entry<String, String> entry : fieldValues.entrySet()) {
                    PDField field = form.getField(entry.getKey());
                    if (field == null) {
                        notFilled.put(entry.getKey(), entry.getValue());
                        continue;
                    }
                    field.setValue(entry.getValue());
                }
            }
            if (!notFilled.isEmpty()) {
                throw new ApiException("Unknown form field(s): " + notFilled.keySet());
            }
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            document.save(out);
            return out.toByteArray();
        } catch (IOException e) {
            throw new ApiException("Failed to fill PDF form: " + e.getMessage(), e);
        }
    }
}
