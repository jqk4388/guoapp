import 'package:flutter/material.dart';

import '../../core_bridge.dart';
import '../app_scope.dart';
import '../widgets/focus_ring.dart';
import '../widgets/form_text_field.dart';

class SourceAccountButton extends StatefulWidget {
  const SourceAccountButton({
    super.key,
    required this.source,
    required this.name,
    required this.admin,
    this.style,
  });
  final String source;
  final String name;
  final bool admin;
  final ButtonStyle? style;

  @override
  State<SourceAccountButton> createState() => _SourceAccountButtonState();
}

class _SourceAccountButtonState extends State<SourceAccountButton> {
  Map<String, dynamic>? _status;
  var _busy = false;

  AppRepository get _repository => AppScope.of(context).repository;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  bool get _loggedIn => _status?['loggedIn'] == true;

  Future<void> _load() async {
    Map<String, dynamic> status;
    try {
      status = await _repository.sourceAccount(widget.source, 'status');
    } catch (_) {
      status = const <String, dynamic>{};
    }
    if (mounted) setState(() => _status = status);
  }

  Future<void> _login() async {
    final status = await showSourceAccountLogin(
      context,
      repository: _repository,
      source: widget.source,
      name: widget.name,
    );
    if (status != null && mounted) setState(() => _status = status);
  }

  Future<void> _logout() async {
    setState(() => _busy = true);
    try {
      final status = await _repository.sourceAccount(widget.source, 'logout');
      if (mounted) setState(() => _status = status);
    } catch (failure) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('$failure')));
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return FocusRing(
      radius: 20,
      child: FilledButton.tonalIcon(
        style: widget.style,
        onPressed: widget.admin && _status != null && !_busy
            ? (_loggedIn ? _logout : _login)
            : null,
        icon: Icon(_loggedIn ? Icons.logout_rounded : Icons.login_rounded),
        label: Text(_loggedIn ? '退出登录' : '登录'),
      ),
    );
  }
}

Future<Map<String, dynamic>?> showSourceAccountLogin(
  BuildContext context, {
  required AppRepository repository,
  required String source,
  required String name,
}) => showDialog<Map<String, dynamic>>(
  context: context,
  barrierDismissible: false,
  builder: (_) => _SourceAccountLoginDialog(
    repository: repository,
    source: source,
    name: name,
  ),
);

class _SourceAccountLoginDialog extends StatefulWidget {
  const _SourceAccountLoginDialog({
    required this.repository,
    required this.source,
    required this.name,
  });
  final AppRepository repository;
  final String source;
  final String name;

  @override
  State<_SourceAccountLoginDialog> createState() =>
      _SourceAccountLoginDialogState();
}

class _SourceAccountLoginDialogState extends State<_SourceAccountLoginDialog> {
  final _account = TextEditingController();
  final _password = TextEditingController();
  final _passwordFocus = FocusNode();
  var _busy = false;
  String? _error;

  @override
  void dispose() {
    _account.dispose();
    _password.dispose();
    _passwordFocus.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_busy) return;
    final account = _account.text.trim();
    final password = _password.text;
    if (account.isEmpty || password.isEmpty) {
      setState(() => _error = '请输入账号和密码');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    final repository = widget.repository;
    try {
      final status = await repository.sourceAccount(
        widget.source,
        'login',
        payload: {'account': account, 'password': password},
      );
      if (mounted) Navigator.pop(context, status);
    } catch (failure) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = '$failure';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text('登录${widget.name}'),
      content: SizedBox(
        width: 360,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          spacing: 12,
          children: [
            FormTextField(
              controller: _account,
              autofocus: true,
              maxLength: 128,
              onSubmitted: (_) => _passwordFocus.requestFocus(),
              decoration: const InputDecoration(labelText: '账号或邮箱'),
            ),
            FormTextField(
              controller: _password,
              focusNode: _passwordFocus,
              obscure: true,
              maxLength: 128,
              onSubmitted: (_) => _submit(),
              decoration: const InputDecoration(labelText: '密码'),
            ),
            if (_error case final error?)
              Text(
                error,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: _busy ? null : _submit,
          child: Text(_busy ? '登录中…' : '登录'),
        ),
      ],
    );
  }
}
