import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

/// The TRACES app mark.
///
/// The artwork comes from `assets/branding/traces_icon.svg`, which carries the
/// same geometry as the web interface's logo and favicon, so the brand stays
/// identical on every platform. The rounded clip and soft drop shadow let it
/// read as an app icon instead of a flat rectangle.
class TracesLogo extends StatelessWidget {
  const TracesLogo({super.key, this.size = 64});

  /// Path of the shared brand asset, relative to the package root.
  static const String assetPath = 'assets/branding/traces_icon.svg';

  /// Edge length of the square mark in logical pixels.
  final double size;

  @override
  Widget build(BuildContext context) {
    final radius = BorderRadius.circular(size * 0.2);
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        borderRadius: radius,
        // Shadow tinted by the theme so the mark sits on light and dark
        // surfaces alike.
        boxShadow: [
          BoxShadow(
            color: Theme.of(context).colorScheme.shadow.withValues(alpha: 0.3),
            blurRadius: size * 0.16,
            offset: Offset(0, size * 0.06),
          ),
        ],
      ),
      child: ClipRRect(
        borderRadius: radius,
        child: SvgPicture.asset(
          assetPath,
          width: size,
          height: size,
          semanticsLabel: 'TRACES',
        ),
      ),
    );
  }
}
