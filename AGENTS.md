# OpenSync contributor workflow

For recurring development operations, create or extend a dedicated Python script so the work can be repeated without reconstructing commands. Keep Python automation in development tooling and keep the shipped application runtime independent of Python.

Use `python tools/dev_desktop.py --run` for the Windows development build. Press F12 in the app to open WebView2 DevTools. Use `--project <path>` with `--run` to open a saved project. The development executable resolves FFmpeg and FFprobe from PATH when they are not bundled beside it.
