package org.opencozy.display;

import java.net.URI;
import java.net.URISyntaxException;
import java.util.Locale;

/**
 * The OpenCozy server this display shows: one HTTPS origin. The WebView may go anywhere on it and
 * nowhere else; other hosts and schemes are refused without handing the link to another app.
 */
public final class ServerAddress {
    private static final int HTTPS_PORT = 443;

    private final String origin;
    private final String host;
    private final int port;

    private ServerAddress(String origin, String host, int port) {
        this.origin = origin;
        this.host = host;
        this.port = port;
    }

    /**
     * What a person types: "family.example.test", "https://family.example.test/" or with a port.
     * Only HTTPS, no credentials, path, query or fragment. Throws IllegalArgumentException.
     */
    public static ServerAddress parse(String typed) {
        if (typed == null || typed.trim().isEmpty()) {
            throw new IllegalArgumentException("An address is required");
        }
        String text = typed.trim();
        if (!text.contains("://")) {
            text = "https://" + text;
        }
        final URI uri;
        try {
            uri = new URI(text);
        } catch (URISyntaxException e) {
            throw new IllegalArgumentException("Not an address", e);
        }
        String path = uri.getRawPath();
        int port = uri.getPort();
        if (!"https".equalsIgnoreCase(uri.getScheme())
                || uri.getHost() == null
                || uri.getUserInfo() != null
                || uri.getRawQuery() != null
                || uri.getRawFragment() != null
                || (path != null && !path.isEmpty() && !"/".equals(path))
                || port == 0 || port > 65535) {
            throw new IllegalArgumentException("Only an https:// address with no path");
        }
        String h = uri.getHost().toLowerCase(Locale.ROOT);
        int p = port == -1 ? HTTPS_PORT : port;
        String o = "https://" + (h.contains(":") && !h.startsWith("[") ? "[" + h + "]" : h) + (p == HTTPS_PORT ? "" : ":" + p);
        return new ServerAddress(o, h, p);
    }

    /** "https://family.example.test", as saved. */
    public String origin() {
        return origin;
    }

    /** The wall display's page: it pairs with a code the first time, then shows the day. */
    public String displayUrl() {
        return origin + "/display";
    }

    /** Whether the WebView may load this URL: HTTPS on this host and port only. */
    public boolean allows(String url) {
        if (url == null) {
            return false;
        }
        try {
            URI u = new URI(url);
            int p = u.getPort() == -1 ? HTTPS_PORT : u.getPort();
            return "https".equalsIgnoreCase(u.getScheme())
                    && u.getUserInfo() == null
                    && u.getHost() != null
                    && host.equalsIgnoreCase(u.getHost())
                    && p == port;
        } catch (URISyntaxException e) {
            return false;
        }
    }
}
