package com.tokensaver.util;

import com.tokensaver.common.ApiException;
import org.springframework.stereotype.Service;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;
import java.util.Set;

@Service
public class HashService {

    private static final Set<String> SUPPORTED = Set.of("MD5", "SHA-1", "SHA-256", "SHA-512");

    public String hash(String text, String algorithm) {
        if (text == null) {
            throw new ApiException("text must not be null");
        }
        String algo = normalize(algorithm);
        try {
            MessageDigest digest = MessageDigest.getInstance(algo);
            byte[] result = digest.digest(text.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(result);
        } catch (NoSuchAlgorithmException e) {
            throw new ApiException("Unsupported algorithm: " + algorithm);
        }
    }

    private String normalize(String algorithm) {
        String algo = (algorithm == null ? "SHA-256" : algorithm).trim().toUpperCase().replace("SHA", "SHA-").replace("SHA--", "SHA-");
        if (!SUPPORTED.contains(algo)) {
            throw new ApiException("Unsupported algorithm: " + algorithm + " (supported: " + SUPPORTED + ")");
        }
        return algo;
    }
}
