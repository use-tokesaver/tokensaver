package com.tokensaver.sheet;

import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

import java.util.Map;

@RestController
public class SheetController {

    private final FormulaService formulaService;

    public SheetController(FormulaService formulaService) {
        this.formulaService = formulaService;
    }

    public record EvaluateRequest(Map<String, String> cells, String formula) {
    }

    /**
     * POST /api/sheet/evaluate  { "cells": {"A1":"5","A2":"10"}, "formula": "=A1+A2" }
     * Evaluates a spreadsheet formula against the given cell values via Apache POI's
     * formula engine, instead of the agent writing/hand-computing it.
     */
    @PostMapping("/api/sheet/evaluate")
    public Map<String, String> evaluate(@RequestBody EvaluateRequest request) {
        return Map.of("result", formulaService.evaluate(request.cells(), request.formula()));
    }
}
