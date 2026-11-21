package org.opencozy.display;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.SharedPreferences;
import android.graphics.Bitmap;
import android.net.http.SslError;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.View;
import android.view.WindowManager;
import android.view.inputmethod.EditorInfo;
import android.webkit.CookieManager;
import android.webkit.RenderProcessGoneDetail;
import android.webkit.SslErrorHandler;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebStorage;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;

/**
 * The wall display: a full-screen WebView on the household's OpenCozy server, at /display. The page
 * pairs itself with a code the first time and keeps its session in the WebView's cookies; this app
 * only holds the server's address. No JavaScript bridge, no file access, no other hosts.
 */
@SuppressLint("SetJavaScriptEnabled")
public final class MainActivity extends Activity {
    private static final String PREFS = "display";
    private static final String KEY_SERVER = "server";
    /** Nobody stands at a wall display to tap "Try again": it tries by itself this often. */
    private static final long RETRY_MS = 30_000;

    private final Handler handler = new Handler(Looper.getMainLooper());
    private final Runnable retry = this::load;
    private WebView web;
    private View setup;
    private View problem;
    private TextView problemText;
    private EditText address;
    private TextView addressError;
    private ServerAddress server;

    @Override
    protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        setContentView(R.layout.activity_main);
        web = findViewById(R.id.web);
        setup = findViewById(R.id.setup);
        problem = findViewById(R.id.problem);
        problemText = findViewById(R.id.problem_text);
        address = findViewById(R.id.address);
        addressError = findViewById(R.id.address_error);

        Button connect = findViewById(R.id.connect);
        connect.setOnClickListener(v -> connect());
        address.setOnEditorActionListener((v, action, event) -> {
            if (action == EditorInfo.IME_ACTION_GO) {
                connect();
                return true;
            }
            return false;
        });
        findViewById(R.id.try_again).setOnClickListener(v -> load());
        findViewById(R.id.change_server).setOnClickListener(v -> forgetServer());

        configure();
        String origin = prefs().getString(KEY_SERVER, null);
        if (origin == null) {
            showSetup();
            return;
        }
        server = ServerAddress.parse(origin);
        load();
    }

    private SharedPreferences prefs() {
        return getSharedPreferences(PREFS, MODE_PRIVATE);
    }

    private void configure() {
        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setAllowFileAccess(false);
        s.setAllowContentAccess(false);
        s.setJavaScriptCanOpenWindowsAutomatically(false);
        s.setSupportMultipleWindows(false);
        s.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        s.setSafeBrowsingEnabled(true);
        CookieManager.getInstance().setAcceptCookie(true);
        CookieManager.getInstance().setAcceptThirdPartyCookies(web, false);
        WebView.setWebContentsDebuggingEnabled(false);
        web.setWebViewClient(new Client());
    }

    private void showSetup() {
        handler.removeCallbacks(retry);
        web.setVisibility(View.GONE);
        problem.setVisibility(View.GONE);
        setup.setVisibility(View.VISIBLE);
        addressError.setVisibility(View.GONE);
        if (address.getText().length() == 0) {
            address.setText(BuildConfig.SUGGESTED_ORIGIN);
        }
        address.requestFocus();
    }

    private void connect() {
        try {
            server = ServerAddress.parse(address.getText().toString());
        } catch (IllegalArgumentException e) {
            addressError.setVisibility(View.VISIBLE);
            return;
        }
        prefs().edit().putString(KEY_SERVER, server.origin()).apply();
        load();
    }

    /** A different server: its session and storage go too. */
    private void forgetServer() {
        prefs().edit().remove(KEY_SERVER).apply();
        server = null;
        web.loadUrl("about:blank");
        CookieManager.getInstance().removeAllCookies(null);
        CookieManager.getInstance().flush();
        WebStorage.getInstance().deleteAllData();
        showSetup();
    }

    /** Always a fresh GET of the display page, never a replay of whatever failed. */
    private void load() {
        handler.removeCallbacks(retry);
        if (server == null) {
            showSetup();
            return;
        }
        setup.setVisibility(View.GONE);
        problem.setVisibility(View.GONE);
        web.setVisibility(View.VISIBLE);
        web.loadUrl(server.displayUrl());
    }

    private void showProblem(int message) {
        problemText.setText(message);
        web.setVisibility(View.GONE);
        problem.setVisibility(View.VISIBLE);
        handler.removeCallbacks(retry);
        handler.postDelayed(retry, RETRY_MS);
    }

    private final class Client extends WebViewClient {
        @Override
        public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
            if (server != null && server.allows(request.getUrl().toString())) {
                return false;
            }
            // Another site, or an app link: never opened. The display stays where it is.
            return true;
        }

        @Override
        public void onPageStarted(WebView view, String url, Bitmap favicon) {
            if (server != null && !"about:blank".equals(url) && !server.allows(url)) {
                view.stopLoading();
                load();
            }
        }

        @Override
        public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
            if (request.isForMainFrame()) {
                showProblem(R.string.problem_network);
            }
        }

        @Override
        public void onReceivedHttpError(WebView view, WebResourceRequest request, WebResourceResponse response) {
            if (request.isForMainFrame() && response.getStatusCode() >= 500) {
                showProblem(R.string.problem_server);
            }
        }

        @Override
        public void onReceivedSslError(WebView view, SslErrorHandler handler, SslError error) {
            handler.cancel();
            showProblem(R.string.problem_tls);
        }

        @Override
        public boolean onRenderProcessGone(WebView view, RenderProcessGoneDetail detail) {
            // The page's renderer crashed or was killed for memory: start over rather than exit.
            recreate();
            return true;
        }
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) {
            getWindow().getDecorView().setSystemUiVisibility(
                    View.SYSTEM_UI_FLAG_FULLSCREEN
                            | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                            | View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                            | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                            | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                            | View.SYSTEM_UI_FLAG_LAYOUT_STABLE);
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        web.onResume();
    }

    @Override
    protected void onPause() {
        web.onPause();
        CookieManager.getInstance().flush();
        super.onPause();
    }

    @Override
    protected void onDestroy() {
        handler.removeCallbacks(retry);
        CookieManager.getInstance().flush();
        web.destroy();
        super.onDestroy();
    }

    @Override
    @SuppressWarnings("deprecation")
    public void onBackPressed() {
        // Back moves within the display's pages; it doesn't close the display.
        if (web.getVisibility() == View.VISIBLE && web.canGoBack()) {
            web.goBack();
        }
    }
}
