package com.tokensaver.sheet;

import com.tokensaver.common.ApiException;
import org.apache.poi.ss.usermodel.Cell;
import org.apache.poi.ss.usermodel.CellValue;
import org.apache.poi.ss.usermodel.FormulaEvaluator;
import org.apache.poi.ss.usermodel.Row;
import org.apache.poi.ss.usermodel.Sheet;
import org.apache.poi.ss.util.CellReference;
import org.apache.poi.xssf.usermodel.XSSFWorkbook;
import org.springframework.stereotype.Service;

import java.io.IOException;
import java.util.Map;

/**
 * Evaluates a spreadsheet formula against a set of cell values via Apache POI's formula
 * engine (the same engine Excel/LibreOffice-compatible tools use) — instead of the agent
 * writing an openpyxl script (which can't itself evaluate formulas) or hand-computing it.
 */
@Service
public class FormulaService {

    /**
     * @param cells   map of cell reference (e.g. "A1") to its literal value (a number if
     *                parseable, otherwise treated as a string)
     * @param formula the formula to evaluate, e.g. "=SUM(A1:A3)" or "=A1*B1"
     */
    public String evaluate(Map<String, String> cells, String formula) {
        if (formula == null || formula.isBlank()) {
            throw new ApiException("formula must not be blank");
        }
        String normalizedFormula = formula.startsWith("=") ? formula.substring(1) : formula;

        try (XSSFWorkbook workbook = new XSSFWorkbook()) {
            Sheet sheet = workbook.createSheet("sheet");
            if (cells != null) {
                for (Map.Entry<String, String> entry : cells.entrySet()) {
                    setCell(sheet, entry.getKey(), entry.getValue());
                }
            }

            CellReference resultRef = new CellReference("ZZ9999");
            Row row = sheet.getRow(resultRef.getRow());
            if (row == null) {
                row = sheet.createRow(resultRef.getRow());
            }
            Cell formulaCell = row.createCell(resultRef.getCol());
            formulaCell.setCellFormula(normalizedFormula);

            FormulaEvaluator evaluator = workbook.getCreationHelper().createFormulaEvaluator();
            CellValue value = evaluator.evaluate(formulaCell);
            return renderValue(value);
        } catch (IOException | RuntimeException e) {
            throw new ApiException("Failed to evaluate formula: " + e.getMessage(), e);
        }
    }

    private void setCell(Sheet sheet, String ref, String rawValue) {
        CellReference cellRef;
        try {
            cellRef = new CellReference(ref);
        } catch (RuntimeException e) {
            throw new ApiException("Invalid cell reference: " + ref);
        }
        Row row = sheet.getRow(cellRef.getRow());
        if (row == null) {
            row = sheet.createRow(cellRef.getRow());
        }
        Cell cell = row.createCell(cellRef.getCol());
        if (rawValue == null) {
            return;
        }
        try {
            cell.setCellValue(Double.parseDouble(rawValue));
        } catch (NumberFormatException e) {
            cell.setCellValue(rawValue);
        }
    }

    private String renderValue(CellValue value) {
        return switch (value.getCellType()) {
            case NUMERIC -> {
                double d = value.getNumberValue();
                yield d == Math.rint(d) && !Double.isInfinite(d)
                        ? String.valueOf((long) d)
                        : String.valueOf(d);
            }
            case STRING -> value.getStringValue();
            case BOOLEAN -> String.valueOf(value.getBooleanValue());
            case ERROR -> throw new ApiException("Formula evaluated to an error: "
                    + org.apache.poi.ss.usermodel.FormulaError.forInt(value.getErrorValue()).getString());
            default -> "";
        };
    }
}
