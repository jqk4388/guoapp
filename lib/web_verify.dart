import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';

import 'core_bridge.dart';

/// 有些站源的播放地址藏在广告短链后面（例如 ouo.io），短链带 Cloudflare 托管挑战，
/// 只有浏览器内核能过去。这里用一个隐藏的 WebView 走完短链验证，把最终地址交回站源模块。
///
/// 站源模块把 `videoUrl` 写成 `browser://<短链地址>`，播放前由 [WebVerify] 换成真实地址。
class WebVerify {
  WebVerify._();

  static final WebVerify instance = WebVerify._();

  static const marker = 'browser://';

  static const shortHosts = {'ouo.io', 'ouo.press', 'ouo.pw'};

  static const blockedHosts = {
    'pubadx.one',
    'adsco.re',
    'weedsyearday.com',
    'cdn4ads.com',
    'magsrv.com',
    'googletagmanager.com',
    'google-analytics.com',
    'doubleclick.net',
    'googlesyndication.com',
    'cloudflareinsights.com',
  };

  WebVerifyHostState? _host;

  void _attach(WebVerifyHostState host) => _host = host;

  void _detach(WebVerifyHostState host) {
    if (identical(_host, host)) _host = null;
  }

  bool get ready => _host != null;

  /// 当前平台是否有可用的浏览器内核（Linux 桌面没有实现）。
  static bool get supported {
    switch (defaultTargetPlatform) {
      case TargetPlatform.android:
      case TargetPlatform.iOS:
      case TargetPlatform.macOS:
      case TargetPlatform.windows:
        return true;
      default:
        return false;
    }
  }

  /// 走完短链验证，返回最终落地地址；失败抛 [AppFailure]。
  Future<String> resolve(String url) async {
    final address = url.trim();
    if (address.isEmpty) {
      throw AppFailure('验证地址为空，请重新播放');
    }
    if (!supported) {
      throw AppFailure('当前平台没有浏览器内核，无法完成验证');
    }
    final host = _host;
    if (host == null) {
      throw AppFailure('浏览器内核没有就绪，请回到首页后重试');
    }
    return host.resolve(address);
  }

  static bool isShortHost(String host) => shortHosts.contains(host.toLowerCase());

  static bool isBlockedHost(String host) {
    final name = host.toLowerCase();
    if (name.isEmpty) return false;
    for (final blocked in blockedHosts) {
      if (name == blocked || name.endsWith('.$blocked')) return true;
    }
    return false;
  }
}

/// 挂在应用根部的隐藏浏览器，只在需要验证时才创建；验证完成后保留实例以复用 cookie。
class WebVerifyHost extends StatefulWidget {
  const WebVerifyHost({super.key, required this.child});

  final Widget child;

  @override
  State<WebVerifyHost> createState() => WebVerifyHostState();
}

class WebVerifyHostState extends State<WebVerifyHost> {
  InAppWebViewController? _controller;
  Completer<String>? _job;
  Timer? _timeout;
  Timer? _retry;
  String? _queued;
  String _lastShort = '';
  int _attempts = 0;
  bool _active = false;
  Widget? _view;

  @override
  void initState() {
    super.initState();
    WebVerify.instance._attach(this);
  }

  @override
  void dispose() {
    WebVerify.instance._detach(this);
    _timeout?.cancel();
    _retry?.cancel();
    final job = _job;
    _job = null;
    if (job != null && !job.isCompleted) {
      job.completeError(AppFailure('验证已取消'));
    }
    super.dispose();
  }

  Future<String> resolve(String url) {
    if (_job != null) {
      throw AppFailure('上一次验证还没结束，请稍后重试');
    }
    final completer = Completer<String>();
    _job = completer;
    _attempts = 0;
    _lastShort = '';
    _timeout?.cancel();
    _timeout = Timer(const Duration(seconds: 90), () {
      _complete('', '验证超时，请检查网络后重试');
    });
    final controller = _controller;
    if (controller == null) {
      _queued = url;
      setState(() => _active = true);
    } else {
      unawaited(_load(controller, url));
    }
    return completer.future;
  }

  Future<void> _load(InAppWebViewController controller, String url) async {
    try {
      await controller.loadUrl(urlRequest: URLRequest(url: WebUri(url)));
    } catch (error) {
      _complete('', '无法打开验证页面：$error');
    }
  }

  void _complete(String value, [String? message]) {
    final job = _job;
    _job = null;
    _timeout?.cancel();
    _timeout = null;
    _retry?.cancel();
    _retry = null;
    if (job == null || job.isCompleted) return;
    if (value.isEmpty) {
      job.completeError(AppFailure(message ?? '验证失败，请重试'));
    } else {
      job.complete(value);
    }
  }

  Future<void> _submit(InAppWebViewController controller) async {
    try {
      await controller.evaluateJavascript(
        source: '''
(function () {
  var form = document.querySelector('#form-captcha')
    || document.querySelector('#form-go')
    || document.querySelector('form[action*="/xreallcygo/"]')
    || document.querySelector('form[action*="/go/"]')
    || document.querySelector('form');
  if (!form) return 'no-form';
  form.submit();
  return 'submitted';
})();
''',
      );
    } catch (_) {}
  }

  void _handleStop(InAppWebViewController controller, WebUri? url) {
    if (_job == null) return;
    final current = url?.toString() ?? '';
    if (current.isEmpty) return;
    final uri = Uri.tryParse(current);
    if (uri == null || uri.host.isEmpty) return;
    if (WebVerify.isShortHost(uri.host)) {
      _attempts++;
      if (_attempts > 6) {
        _complete('', '短链验证没有通过，请稍后重试');
        return;
      }
      _lastShort = current;
      unawaited(_submit(controller));
      _retry?.cancel();
      _retry = Timer(const Duration(milliseconds: 2200), () {
        if (_job == null || _lastShort != current) return;
        unawaited(_submit(controller));
      });
      return;
    }
    if (WebVerify.isBlockedHost(uri.host)) return;
    _complete(current);
  }

  InAppWebView _buildView() => InAppWebView(
    initialUrlRequest: URLRequest(url: WebUri('about:blank')),
    initialSettings: InAppWebViewSettings(
      javaScriptEnabled: true,
      thirdPartyCookiesEnabled: true,
      mediaPlaybackRequiresUserGesture: false,
      supportMultipleWindows: false,
      useShouldOverrideUrlLoading: true,
    ),
    onWebViewCreated: (controller) {
      _controller = controller;
      final queued = _queued;
      _queued = null;
      if (queued != null) unawaited(_load(controller, queued));
    },
    shouldOverrideUrlLoading: (controller, action) async =>
        WebVerify.isBlockedHost(action.request.url?.host ?? '')
        ? NavigationActionPolicy.CANCEL
        : NavigationActionPolicy.ALLOW,
    onCreateWindow: (controller, action) async => false,
    onLoadStop: (controller, url) => _handleStop(controller, url),
    onReceivedError: (controller, request, error) {
      if (request.isForMainFrame != true || _job == null) return;
      final host = request.url.host;
      if (host.isEmpty || WebVerify.isBlockedHost(host)) return;
      _complete('', '验证页面加载失败：${error.description}');
    },
  );

  @override
  Widget build(BuildContext context) {
    _view ??= _buildView();
    return Stack(
      alignment: Alignment.topLeft,
      children: [
        widget.child,
        if (_active)
          Positioned(
            right: 0,
            bottom: 0,
            width: 2,
            height: 2,
            child: IgnorePointer(
              child: ClipRect(child: Opacity(opacity: 0.01, child: _view)),
            ),
          ),
      ],
    );
  }
}
