import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Renders an [AsyncValue] with consistent loading and error states.
class AsyncValueView<T> extends StatelessWidget {
  const AsyncValueView({
    super.key,
    required this.value,
    required this.builder,
    this.onRetry,
    this.loading,
    this.empty,
    this.isEmpty,
  });

  final AsyncValue<T> value;
  final Widget Function(BuildContext context, T data) builder;
  final Future<void> Function()? onRetry;
  final Widget? loading;
  final Widget? empty;
  final bool Function(T data)? isEmpty;

  @override
  Widget build(BuildContext context) {
    final current = value;

    if (current.hasValue) {
      final data = current.value as T;
      if (isEmpty?.call(data) ?? false) {
        return empty ?? const EmptyView();
      }
      return builder(context, data);
    }
    if (current.hasError) {
      return ErrorRetryView(
        message: '${current.error}',
        onRetry: onRetry,
      );
    }
    return loading ?? const Center(child: CircularProgressIndicator());
  }
}

/// Error state with a retry affordance.
class ErrorRetryView extends StatelessWidget {
  const ErrorRetryView({super.key, required this.message, this.onRetry});

  final String message;
  final Future<void> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.cloud_off, size: 48, color: scheme.error),
            const SizedBox(height: 12),
            Text(
              message,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            if (onRetry != null) ...[
              const SizedBox(height: 16),
              FilledButton.tonalIcon(
                onPressed: () => onRetry!.call(),
                icon: const Icon(Icons.refresh),
                label: const Text('Retry'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Neutral "nothing here yet" state.
class EmptyView extends StatelessWidget {
  const EmptyView({super.key, this.message = 'Nothing to show yet'});

  final String message;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.inbox_outlined, size: 48, color: scheme.outline),
            const SizedBox(height: 12),
            Text(
              message,
              textAlign: TextAlign.center,
              style: Theme.of(context)
                  .textTheme
                  .bodyMedium
                  ?.copyWith(color: scheme.outline),
            ),
          ],
        ),
      ),
    );
  }
}
