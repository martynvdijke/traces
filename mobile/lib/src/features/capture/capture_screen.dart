import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';
import 'package:image_picker/image_picker.dart';
import 'package:intl/intl.dart';

import '../../data/api/api_exception.dart';
import '../../data/models/timeline_event.dart';
import '../../state/providers.dart';

/// Mobile memory logger: capture a photo/video/audio clip, attach GPS and
/// people, and upload it straight to the timeline.
class CaptureScreen extends ConsumerStatefulWidget {
  const CaptureScreen({super.key});

  @override
  ConsumerState<CaptureScreen> createState() => _CaptureScreenState();
}

class _CaptureScreenState extends ConsumerState<CaptureScreen> {
  final ImagePicker _picker = ImagePicker();
  final TextEditingController _title = TextEditingController();
  final TextEditingController _description = TextEditingController();
  final TextEditingController _location = TextEditingController();
  final TextEditingController _tags = TextEditingController();

  String? _filePath;
  String _mediaType = '';
  DateTime _date = DateTime.now();
  double? _latitude;
  double? _longitude;
  bool _locating = false;
  int? _personId;

  bool _submitting = false;
  double _progress = 0;
  String? _error;
  String? _notice;

  @override
  void dispose() {
    _title.dispose();
    _description.dispose();
    _location.dispose();
    _tags.dispose();
    super.dispose();
  }

  bool get _isImage => _mediaType == 'image';

  String get _fileName =>
      _filePath == null ? '' : _filePath!.split('/').last;

  Future<void> _pickImage(ImageSource source) async {
    try {
      final file = await _picker.pickImage(
        source: source,
        imageQuality: 90,
        maxWidth: 4096,
      );
      if (file == null) return;
      _setDraft(file.path, 'image');
    } catch (error) {
      _fail('Could not open $error');
    }
  }

  Future<void> _pickVideo() async {
    try {
      final file = await _picker.pickVideo(source: ImageSource.gallery);
      if (file == null) return;
      _setDraft(file.path, 'video');
    } catch (error) {
      _fail('Could not open $error');
    }
  }

  Future<void> _pickAudio() async {
    try {
      final result = await FilePicker.pickFile(type: FileType.audio);
      final path = result?.path;
      if (path == null) return;
      _setDraft(path, 'audio');
    } catch (error) {
      _fail('Could not open $error');
    }
  }

  void _setDraft(String path, String mediaType) {
    setState(() {
      _filePath = path;
      _mediaType = mediaType;
      _error = null;
      _notice = null;
    });
    _locate();
  }

  void _clearDraft() {
    setState(() {
      _filePath = null;
      _mediaType = '';
    });
  }

  void _fail(String message) {
    if (!mounted) return;
    setState(() => _error = message);
  }

  /// Resolves the current position and attaches it to the pending memory.
  Future<void> _locate() async {
    setState(() {
      _locating = true;
      _notice = 'Locating…';
    });
    try {
      if (!await Geolocator.isLocationServiceEnabled()) {
        _finishLocate('Location services are disabled — GPS not attached');
        return;
      }
      var permission = await Geolocator.checkPermission();
      if (permission == LocationPermission.denied) {
        permission = await Geolocator.requestPermission();
      }
      if (permission == LocationPermission.denied ||
          permission == LocationPermission.deniedForever) {
        _finishLocate('Location permission denied — GPS not attached');
        return;
      }
      final position = await Geolocator.getCurrentPosition(
        locationSettings: const LocationSettings(
          accuracy: LocationAccuracy.high,
          timeLimit: Duration(seconds: 20),
        ),
      );
      if (!mounted) return;
      setState(() {
        _latitude = position.latitude;
        _longitude = position.longitude;
        _locating = false;
        _notice = 'GPS attached '
            '(${position.latitude.toStringAsFixed(5)}, '
            '${position.longitude.toStringAsFixed(5)})';
      });
    } catch (error) {
      _finishLocate('Could not determine location: $error');
    }
  }

  void _finishLocate(String message) {
    if (!mounted) return;
    setState(() {
      _locating = false;
      _notice = message;
    });
  }

  Future<void> _pickDate() async {
    final picked = await showDatePicker(
      context: context,
      initialDate: _date,
      firstDate: DateTime(1900),
      lastDate: DateTime.now().add(const Duration(days: 365)),
    );
    if (picked != null) setState(() => _date = picked);
  }

  Future<void> _submit() async {
    FocusScope.of(context).unfocus();
    final title = _title.text.trim();
    if (_filePath == null) {
      _fail('Pick a photo, video, or audio clip first');
      return;
    }
    if (title.isEmpty) {
      _fail('Give this memory a title');
      return;
    }

    setState(() {
      _submitting = true;
      _progress = 0;
      _error = null;
      _notice = 'Uploading $_fileName…';
    });

    try {
      final upload = await ref.read(mediaRepositoryProvider).upload(
            filePath: _filePath!,
            mediaType: _mediaType,
            onProgress: (sent, total) {
              if (!mounted || total <= 0) return;
              setState(() => _progress = (sent / total).clamp(0, 1));
            },
          );
      if (!mounted) return;

      final latitude = upload.latitude ?? _latitude;
      final longitude = upload.longitude ?? _longitude;
      final trimmedLocation = _location.text.trim();

      final event = TimelineEvent(
        title: title,
        description: _description.text.trim(),
        date: DateFormat('yyyy-MM-dd').format(_date),
        location: trimmedLocation.isNotEmpty
            ? trimmedLocation
            : upload.locationSuggestion,
        mediaType: upload.mediaType.isEmpty ? _mediaType : upload.mediaType,
        mediaUrl: upload.url,
        thumbnail: upload.thumbnail,
        tags: _tags.text.trim(),
        latitude: latitude,
        longitude: longitude,
        personId: _personId,
      );
      await ref.read(eventsRepositoryProvider).saveEvent(event);
      if (!mounted) return;

      ref.read(selectedYearProvider.notifier).select(_date.year);
      ref.invalidate(timelineProvider);
      ref.invalidate(mapDataProvider);
      ref.invalidate(contributionsProvider);
      ref.invalidate(tagsProvider);
      ref.invalidate(yearStatsProvider);
      ref.invalidate(personEventsProvider);

      setState(() {
        _submitting = false;
        _progress = 0;
        _filePath = null;
        _mediaType = '';
        _title.clear();
        _description.clear();
        _tags.clear();
        _location.clear();
        _personId = null;
        _date = DateTime.now();
        _latitude = null;
        _longitude = null;
        _notice = 'Saved “$title” to your timeline';
      });
    } on ApiException catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _notice = null;
        _error = error.message;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _notice = null;
        _error = '$error';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final persons = ref.watch(personsProvider).value ?? const [];

    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 96),
      children: [
        _MediaPicker(
          filePath: _filePath,
          mediaType: _mediaType,
          isImage: _isImage,
          fileName: _fileName,
          onCamera: _submitting ? null : () => _pickImage(ImageSource.camera),
          onPhoto: _submitting ? null : () => _pickImage(ImageSource.gallery),
          onVideo: _submitting ? null : _pickVideo,
          onAudio: _submitting ? null : _pickAudio,
          onClear: _submitting ? null : _clearDraft,
        ),
        const SizedBox(height: 16),
        TextField(
          key: const Key('capture-title'),
          controller: _title,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(
            labelText: 'Title',
            border: OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _description,
          maxLines: 3,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(
            labelText: 'Description',
            border: OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 12),
        InkWell(
          onTap: _submitting ? null : _pickDate,
          child: InputDecorator(
            decoration: const InputDecoration(
              labelText: 'Date',
              prefixIcon: Icon(Icons.event),
              border: OutlineInputBorder(),
            ),
            child: Text(DateFormat.yMMMMd().format(_date)),
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _location,
          decoration: const InputDecoration(
            labelText: 'Location',
            hintText: 'Filled from GPS if left empty',
            prefixIcon: Icon(Icons.place_outlined),
            border: OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 8),
        Row(
          children: [
            if (_locating)
              const SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            else
              Icon(
                _latitude != null ? Icons.my_location : Icons.location_searching,
                size: 18,
                color: _latitude != null ? scheme.primary : scheme.outline,
              ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                _latitude != null
                    ? 'GPS ${_latitude!.toStringAsFixed(5)}, '
                        '${_longitude!.toStringAsFixed(5)}'
                    : 'No GPS coordinates attached',
                style: theme.textTheme.labelSmall,
              ),
            ),
            TextButton(
              onPressed: _locating || _submitting ? null : _locate,
              child: Text(_latitude != null ? 'Refresh' : 'Attach GPS'),
            ),
          ],
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _tags,
          decoration: const InputDecoration(
            labelText: 'Tags',
            hintText: 'summer, beach, family',
            prefixIcon: Icon(Icons.sell_outlined),
            border: OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 12),
        InputDecorator(
          decoration: const InputDecoration(
            labelText: 'Person',
            prefixIcon: Icon(Icons.person_outline),
            border: OutlineInputBorder(),
          ),
          child: DropdownButtonHideUnderline(
            child: DropdownButton<int?>(
              isExpanded: true,
              value: _personId,
              items: [
                const DropdownMenuItem<int?>(
                  value: null,
                  child: Text('No person'),
                ),
                for (final person in persons)
                  DropdownMenuItem<int?>(
                    value: person.id,
                    child: Text(person.name),
                  ),
              ],
              onChanged: _submitting
                  ? null
                  : (value) => setState(() => _personId = value),
            ),
          ),
        ),
        const SizedBox(height: 20),
        if (_submitting)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: LinearProgressIndicator(
              value: _progress == 0 ? null : _progress,
            ),
          ),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: Text(_error!, style: TextStyle(color: scheme.error)),
          )
        else if (_notice != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: Text(
              _notice!,
              style: theme.textTheme.bodySmall?.copyWith(color: scheme.primary),
            ),
          ),
        FilledButton.icon(
          key: const Key('capture-submit'),
          onPressed: _submitting ? null : _submit,
          icon: const Icon(Icons.cloud_upload_outlined),
          label: Text(_submitting ? 'Uploading…' : 'Save memory'),
        ),
      ],
    );
  }
}

class _MediaPicker extends StatelessWidget {
  const _MediaPicker({
    required this.filePath,
    required this.mediaType,
    required this.isImage,
    required this.fileName,
    required this.onCamera,
    required this.onPhoto,
    required this.onVideo,
    required this.onAudio,
    required this.onClear,
  });

  final String? filePath;
  final String mediaType;
  final bool isImage;
  final String fileName;
  final VoidCallback? onCamera;
  final VoidCallback? onPhoto;
  final VoidCallback? onVideo;
  final VoidCallback? onAudio;
  final VoidCallback? onClear;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;

    if (filePath == null) {
      return Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'Add media',
                style: Theme.of(context).textTheme.titleSmall,
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  FilledButton.tonalIcon(
                    key: const Key('capture-camera'),
                    onPressed: onCamera,
                    icon: const Icon(Icons.photo_camera_outlined),
                    label: const Text('Camera'),
                  ),
                  FilledButton.tonalIcon(
                    key: const Key('capture-photo'),
                    onPressed: onPhoto,
                    icon: const Icon(Icons.photo_library_outlined),
                    label: const Text('Photo'),
                  ),
                  FilledButton.tonalIcon(
                    key: const Key('capture-video'),
                    onPressed: onVideo,
                    icon: const Icon(Icons.videocam_outlined),
                    label: const Text('Video'),
                  ),
                  FilledButton.tonalIcon(
                    key: const Key('capture-audio'),
                    onPressed: onAudio,
                    icon: const Icon(Icons.mic_none),
                    label: const Text('Audio'),
                  ),
                ],
              ),
            ],
          ),
        ),
      );
    }

    return Card(
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (isImage)
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 260),
              child: Image.file(
                File(filePath!),
                fit: BoxFit.cover,
                errorBuilder: (context, error, stack) => Container(
                  height: 160,
                  color: scheme.surfaceContainerHighest,
                  child: const Icon(Icons.broken_image_outlined, size: 40),
                ),
              ),
            )
          else
            Container(
              height: 120,
              color: scheme.surfaceContainerHighest,
              child: Icon(
                mediaType == 'video'
                    ? Icons.movie_outlined
                    : Icons.audiotrack_outlined,
                size: 48,
                color: scheme.onSurfaceVariant,
              ),
            ),
          ListTile(
            title: Text(fileName, maxLines: 1, overflow: TextOverflow.ellipsis),
            subtitle: Text('$mediaType attachment'),
            trailing: IconButton(
              tooltip: 'Remove',
              icon: const Icon(Icons.close),
              onPressed: onClear,
            ),
          ),
        ],
      ),
    );
  }
}
