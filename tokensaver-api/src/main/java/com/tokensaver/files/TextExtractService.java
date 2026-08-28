package com.tokensaver.files;

import com.tokensaver.common.ApiException;
import org.apache.pdfbox.Loader;
import org.apache.pdfbox.pdmodel.PDDocument;
import org.apache.pdfbox.text.PDFTextStripper;
import org.apache.poi.ss.usermodel.Cell;
import org.apache.poi.ss.usermodel.Row;
import org.apache.poi.ss.usermodel.Sheet;
import org.apache.poi.xssf.usermodel.XSSFWorkbook;
import org.apache.poi.xwpf.usermodel.XWPFDocument;
import org.apache.poi.xwpf.usermodel.XWPFParagraph;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.Locale;

/**
 * Extracts plain text from common document formats — the boilerplate an agent
 * would otherwise reach for PyPDF2/python-docx/openpyxl to do.
 */
@Service
public class TextExtractService {

    public String extract(MultipartFile file) {
        if (file == null || file.isEmpty()) {
            throw new ApiException("file must not be empty");
        }
        String name = file.getOriginalFilename() == null ? "" : file.getOriginalFilename();
        try (InputStream in = file.getInputStream()) {
            return extractFromStream(in, name);
        } catch (IOException e) {
            throw new ApiException("Failed to read uploaded file: " + e.getMessage(), e);
        }
    }

    /** Same extraction, for callers that already have the file's bytes rather than a MultipartFile. */
    public String extract(byte[] content, String filename) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        try (InputStream in = new ByteArrayInputStream(content)) {
            return extractFromStream(in, filename == null ? "" : filename);
        } catch (IOException e) {
            throw new ApiException("Failed to read file: " + e.getMessage(), e);
        }
    }

    private String extractFromStream(InputStream in, String filename) throws IOException {
        String name = filename.toLowerCase(Locale.ROOT);
        if (name.endsWith(".pdf")) {
            return extractPdf(in);
        } else if (name.endsWith(".docx")) {
            return extractDocx(in);
        } else if (name.endsWith(".xlsx")) {
            return extractXlsx(in);
        } else if (name.endsWith(".txt") || name.endsWith(".md") || name.endsWith(".csv")) {
            return new String(in.readAllBytes(), StandardCharsets.UTF_8);
        } else {
            throw new ApiException("Unsupported file type for text extraction: " + name
                    + " (supported: .pdf, .docx, .xlsx, .txt, .md, .csv)");
        }
    }

    private String extractPdf(InputStream in) throws IOException {
        try (PDDocument doc = Loader.loadPDF(in.readAllBytes())) {
            return new PDFTextStripper().getText(doc);
        }
    }

    private String extractDocx(InputStream in) throws IOException {
        try (XWPFDocument doc = new XWPFDocument(in)) {
            StringBuilder sb = new StringBuilder();
            for (XWPFParagraph p : doc.getParagraphs()) {
                sb.append(p.getText()).append("\n");
            }
            return sb.toString();
        }
    }

    private String extractXlsx(InputStream in) throws IOException {
        try (XSSFWorkbook workbook = new XSSFWorkbook(in)) {
            StringBuilder sb = new StringBuilder();
            for (int s = 0; s < workbook.getNumberOfSheets(); s++) {
                Sheet sheet = workbook.getSheetAt(s);
                sb.append("# Sheet: ").append(sheet.getSheetName()).append("\n");
                for (Row row : sheet) {
                    StringBuilder line = new StringBuilder();
                    for (Cell cell : row) {
                        line.append(cellToString(cell)).append("\t");
                    }
                    sb.append(line.toString().stripTrailing()).append("\n");
                }
            }
            return sb.toString();
        }
    }

    private String cellToString(Cell cell) {
        return switch (cell.getCellType()) {
            case STRING -> cell.getStringCellValue();
            case NUMERIC -> String.valueOf(cell.getNumericCellValue());
            case BOOLEAN -> String.valueOf(cell.getBooleanCellValue());
            case FORMULA -> cell.getCellFormula();
            default -> "";
        };
    }
}
