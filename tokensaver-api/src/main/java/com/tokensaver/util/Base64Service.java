package com.tokensaver.util;

import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;

import java.nio.charset.StandardCharsets;
import java.util.Base64;

@Service
public class Base64Service {

    public String encode(String text) {
        if (text == null) {
            throw new ApiException("text must not be null");
        }
        return Base64.getEncoder().encodeToString(text.getBytes(StandardCharsets.UTF_8));
    }

    public String decode(String text) {
        if (text == null) {
            throw new ApiException("text must not be null");
        }
        try {
            return new String(Base64.getDecoder().decode(text), StandardCharsets.UTF_8);
        } catch (IllegalArgumentException e) {
            throw new ApiException("Invalid base64 input: " + e.getMessage(), e);
        }
    }
}
