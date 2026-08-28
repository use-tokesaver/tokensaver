package com.tokensaver.common;

/** Thrown for any request-caused failure (bad input, unreachable URL, unsupported format). */
public class ApiException extends RuntimeException {
    public ApiException(String message) {
        super(message);
    }

    public ApiException(String message, Throwable cause) {
        super(message, cause);
    }
}
