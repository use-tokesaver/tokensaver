package com.tokensaver.data;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.csv.CsvMapper;
import com.fasterxml.jackson.dataformat.csv.CsvSchema;
import com.fasterxml.jackson.dataformat.yaml.YAMLMapper;
import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Converts between JSON/YAML/CSV — the format-juggling an agent otherwise
 * writes a small Python/jq snippet for, every single time.
 */
@Service
public class DataConvertService {

    private final ObjectMapper json = new ObjectMapper();
    private final YAMLMapper yaml = new YAMLMapper();
    private final CsvMapper csv = new CsvMapper();

    public String convert(String input, String fromFormat, String toFormat) {
        if (input == null || input.isBlank()) {
            throw new ApiException("input must not be blank");
        }
        Format from = Format.parse(fromFormat);
        Format to = Format.parse(toFormat);

        JsonNode tree = read(input, from);
        return write(tree, to);
    }

    private JsonNode read(String input, Format format) {
        try {
            return switch (format) {
                case JSON -> json.readTree(input);
                case YAML -> yaml.readTree(input);
                case CSV -> readCsvAsArray(input);
            };
        } catch (Exception e) {
            throw new ApiException("Failed to parse input as " + format + ": " + e.getMessage(), e);
        }
    }

    private String write(JsonNode tree, Format format) {
        try {
            return switch (format) {
                case JSON -> json.writerWithDefaultPrettyPrinter().writeValueAsString(tree);
                case YAML -> yaml.writeValueAsString(tree);
                case CSV -> writeArrayAsCsv(tree);
            };
        } catch (Exception e) {
            throw new ApiException("Failed to write output as " + format + ": " + e.getMessage(), e);
        }
    }

    private JsonNode readCsvAsArray(String input) throws Exception {
        CsvSchema schema = CsvSchema.emptySchema().withHeader();
        List<Map<String, String>> rows = csv.readerFor(Map.class)
                .with(schema)
                .<Map<String, String>>readValues(input)
                .readAll();
        return json.valueToTree(rows);
    }

    private String writeArrayAsCsv(JsonNode tree) throws Exception {
        if (!tree.isArray() || tree.isEmpty()) {
            throw new ApiException("CSV output requires a non-empty JSON/YAML array of flat objects");
        }
        CsvSchema.Builder schemaBuilder = CsvSchema.builder();
        JsonNode first = tree.get(0);
        if (!first.isObject()) {
            throw new ApiException("CSV output requires an array of objects (rows)");
        }
        first.fieldNames().forEachRemaining(schemaBuilder::addColumn);
        CsvSchema schema = schemaBuilder.build().withHeader();

        // Flatten each element to a LinkedHashMap<String,String> matching the header order.
        List<Map<String, Object>> rows = json.convertValue(tree, List.class);
        return csv.writer(schema).writeValueAsString(rows.stream().map(LinkedHashMap::new).toList());
    }

    private enum Format {
        JSON, YAML, CSV;

        static Format parse(String value) {
            if (value == null) {
                throw new ApiException("format must be one of json, yaml, csv");
            }
            try {
                return Format.valueOf(value.trim().toUpperCase());
            } catch (IllegalArgumentException e) {
                throw new ApiException("Unsupported format: " + value + " (supported: json, yaml, csv)");
            }
        }
    }
}
