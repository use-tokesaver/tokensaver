package com.tokensaver.audio;

import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;

import java.util.Map;

@RestController
@RequestMapping("/api/audio")
public class AudioController {

    private final TranscribeService transcribeService;

    public AudioController(TranscribeService transcribeService) {
        this.transcribeService = transcribeService;
    }

    /**
     * POST /api/audio/transcribe  (multipart "file", optional query param "model": tiny/base/small/medium/large)
     * Returns the transcript via the locally installed Whisper CLI, instead of the
     * agent writing a Python whisper script to do it.
     */
    @PostMapping("/transcribe")
    public Map<String, String> transcribe(
            @RequestParam("file") MultipartFile file,
            @RequestParam(value = "model", required = false, defaultValue = "base") String model) {
        return Map.of("text", transcribeService.transcribe(file, model));
    }
}
