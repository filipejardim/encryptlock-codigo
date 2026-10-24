#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <flutter_windows.h>
#include <windows.h>

#include "flutter_window.h"
#include "utils.h"

int APIENTRY wWinMain(_In_ HINSTANCE instance, _In_opt_ HINSTANCE prev,
                      _In_ wchar_t *command_line, _In_ int show_command) {
  // Attach to console when present (e.g., 'flutter run') or create a
  // new console when running with a debugger.
  if (!::AttachConsole(ATTACH_PARENT_PROCESS) && ::IsDebuggerPresent()) {
    CreateAndAttachConsole();
  }

  // Initialize COM, so that it is available for use in the library and/or
  // plugins.
  ::CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);

  flutter::DartProject project(L"data");

  std::vector<std::string> command_line_arguments =
      GetCommandLineArguments();

  project.set_dart_entrypoint_arguments(std::move(command_line_arguments));

  FlutterWindow window(project);
  // Opens as a wide computer window in the middle of the screen (not the
  // whole screen); it can be resized or maximised and the app fills it.
  const unsigned int width = 1280, height = 820;
  const POINT zero = {0, 0};
  const double scale =
      FlutterDesktopGetDpiForMonitor(MonitorFromPoint(zero, MONITOR_DEFAULTTOPRIMARY)) / 96.0;
  RECT work;
  SystemParametersInfo(SPI_GETWORKAREA, 0, &work, 0);
  const int x = static_cast<int>(((work.right - work.left) / scale - width) / 2);
  const int y = static_cast<int>(((work.bottom - work.top) / scale - height) / 2);
  Win32Window::Point origin(x > 0 ? x : 0, y > 0 ? y : 0);
  Win32Window::Size size(width, height);
  if (!window.Create(L"EncryptLock", origin, size)) {
    return EXIT_FAILURE;
  }
  window.SetQuitOnClose(true);

  ::MSG msg;
  while (::GetMessage(&msg, nullptr, 0, 0)) {
    ::TranslateMessage(&msg);
    ::DispatchMessage(&msg);
  }

  ::CoUninitialize();
  return EXIT_SUCCESS;
}
