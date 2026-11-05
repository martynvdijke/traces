package xyz.vandijke.traces

import android.webkit.CookieManager
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private val channelName = "xyz.vandijke.traces/cookies"

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)

        // The server's `session` cookie is HttpOnly, so the OIDC flow in the
        // WebView cannot read it from JavaScript. This channel exposes the
        // native WebView cookie store to Dart instead.
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, channelName)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "getCookies" -> {
                        val url = call.argument<String>("url")
                        if (url.isNullOrEmpty()) {
                            result.error("invalid_url", "A url argument is required", null)
                        } else {
                            result.success(CookieManager.getInstance().getCookie(url))
                        }
                    }
                    "clearCookies" -> {
                        CookieManager.getInstance().removeAllCookies(null)
                        CookieManager.getInstance().flush()
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
    }
}
