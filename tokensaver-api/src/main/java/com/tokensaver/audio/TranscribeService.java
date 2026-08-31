package com.tokensaver.audio;

import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Comparator;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;

/**
 * Transcribes audio by shelling out to the locally installed OpenAI Whisper CLI (a
 * deterministic, locally-run model — not a paid per-token API) — the boilerplate an
 * agent would otherwise reach for a Python whisper script to do.
 *
 * Requires the "whisper" binary on PATH (`pip install openai-whisper`, plus `ffmpeg`
 * for audio decoding). The first run for a given model size downloads model weights
 * once; after that it's fully offline.
 */
@Service
public class TranscribeService {

    private static final long TIMEOUT_SECONDS = 600;

    public String transcribe(MultipartFile file, String model) {
        if (file == null || file.isEmpty()) {
            throw new ApiException("file must not be empty");
        }
        String name = file.getOriginalFilename() == null ? "audio" : file.getOriginalFilename();
        try (InputStream in = file.getInputStream()) {
            return transcribe(in.readAllBytes(), name, model);
        } catch (IOException e) {
            throw new ApiException("Failed to read uploaded file: " + e.getMessage(), e);
        }
    }

    /** Same transcription, for callers that already have the file's bytes rather than a MultipartFile. */
    public String transcribe(byte[] content, String filename, String model) {
        if (content == null || content.length == 0) {
            throw new ApiException("content must not be empty");
        }
        String modelName = (model == null || model.isBlank()) ? "base" : model;
        String suffix = filename != null && filename.contains(".")
                ? filename.substring(filename.lastIndexOf('.'))
                : ".audio";

        Path workDir;
        try {
            workDir = Files.createTempDirectory("tokensaver-whisper-");
        } catch (IOException e) {
            throw new ApiException("Failed to create temp directory for transcription: " + e.getMessage(), e);
        }
        try {
            Path inputFile = workDir.resolve("input" + suffix);
            Files.write(inputFile, content);
            return runWhisper(inputFile, workDir, modelName);
        } catch (IOException e) {
            throw new ApiException("Failed to write temp audio file: " + e.getMessage(), e);
        } finally {
            deleteRecursively(workDir);
        }
    }

    private String runWhisper(Path inputFile, Path workDir, String model) {
        Process process;
        try {
            process = new ProcessBuilder("whisper", inputFile.toString(),
                    "--model", model,
                    "--output_format", "txt",
                    "--output_dir", workDir.toString())
                    .redirectErrorStream(false)
                    .start();
        } catch (IOException e) {
            throw new ApiException("Could not run 'whisper' — is it installed and on PATH? "
                    + "(`pip install openai-whisper`, plus ffmpeg): " + e.getMessage(), e);
        }

        CompletableFuture<String> stdout = CompletableFuture.supplyAsync(() -> readAll(process.getInputStream()));
        CompletableFuture<String> stderr = CompletableFuture.supplyAsync(() -> readAll(process.getErrorStream()));

        boolean finished;
        try {
            finished = process.waitFor(TIMEOUT_SECONDS, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new ApiException("Transcription was interrupted", e);
        }
        if (!finished) {
            process.destroyForcibly();
            throw new ApiException("whisper timed out after " + TIMEOUT_SECONDS + "s");
        }
        stdout.join();
        if (process.exitValue() != 0) {
            throw new ApiException("whisper failed: " + stderr.join());
        }

        Path outputFile = workDir.resolve(stripExtension(inputFile.getFileName().toString()) + ".txt");
        if (!Files.exists(outputFile)) {
            throw new ApiException("whisper did not produce a transcript file");
        }
        try {
            return Files.readString(outputFile, StandardCharsets.UTF_8).strip();
        } catch (IOException e) {
            throw new ApiException("Failed to read transcript: " + e.getMessage(), e);
        }
    }

    private static String stripExtension(String filename) {
        int dot = filename.lastIndexOf('.');
        return dot < 0 ? filename : filename.substring(0, dot);
    }

    private String readAll(InputStream in) {
        try {
            return new String(in.readAllBytes(), StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw new ApiException("Failed to read whisper output: " + e.getMessage(), e);
        }
    }

    private void deleteRecursively(Path dir) {
        try (Stream<Path> paths = Files.walk(dir)) {
            paths.sorted(Comparator.reverseOrder()).forEach(p -> {
                try {
                    Files.deleteIfExists(p);
                } catch (IOException ignored) {
                    // best-effort cleanup
                }
            });
        } catch (IOException ignored) {
            // best-effort cleanup
        }
    }
}
