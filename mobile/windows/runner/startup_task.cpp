#include "startup_task.h"

// windows.h first: appmodel.h needs its definitions.
#include <windows.h>

#include <appmodel.h>
#include <flutter/method_channel.h>
#include <flutter/standard_method_codec.h>

#include <memory>
#include <string>
#include <thread>

#include <winrt/Windows.ApplicationModel.h>
#include <winrt/Windows.Foundation.h>

namespace {

using winrt::Windows::ApplicationModel::StartupTask;
using winrt::Windows::ApplicationModel::StartupTaskState;

// The package's startup tasks (AppxManifest.xml written by scripts/msix.ps1).
constexpr wchar_t kLockKeeperTask[] = L"LockKeeperStart";  // lockkeeper.exe
constexpr wchar_t kAppTask[] = L"AppStart";                // EncryptLock.exe

bool Packaged() {
  UINT32 n = 0;
  LONG r = GetCurrentPackageFullName(&n, nullptr);
  return r == ERROR_INSUFFICIENT_BUFFER;
}

std::string Name(StartupTaskState s) {
  switch (s) {
    case StartupTaskState::Enabled: return "enabled";
    case StartupTaskState::DisabledByUser: return "disabledByUser";
    case StartupTaskState::DisabledByPolicy: return "disabledByPolicy";
    case StartupTaskState::EnabledByPolicy: return "enabledByPolicy";
    default: return "disabled";
  }
}

// The WinRT calls wait for their answer: on a thread of their own (the
// window's thread must not block on them), joined at once - they are quick
// and show no dialog for a desktop app.
std::string Run(const std::string& method, const wchar_t* task_id) {
  std::string out = "unpackaged";
  std::thread worker([&] {
    try {
      winrt::init_apartment(winrt::apartment_type::multi_threaded);
      StartupTask task = StartupTask::GetAsync(task_id).get();
      if (method == "enable") {
        out = Name(task.RequestEnableAsync().get());
      } else {
        if (method == "disable") task.Disable();
        out = Name(task.State());
      }
    } catch (...) {
      out = "error";
    }
  });
  worker.join();
  return out;
}

}  // namespace

void RegisterStartupTaskChannel(flutter::BinaryMessenger* messenger) {
  auto channel = std::make_shared<flutter::MethodChannel<flutter::EncodableValue>>(
      messenger, "encryptlock/startup", &flutter::StandardMethodCodec::GetInstance());
  channel->SetMethodCallHandler(
      [channel](const flutter::MethodCall<flutter::EncodableValue>& call,
                std::unique_ptr<flutter::MethodResult<flutter::EncodableValue>> result) {
        const std::string& m = call.method_name();
        if (m != "state" && m != "enable" && m != "disable") {
          result->NotImplemented();
          return;
        }
        // Argument "task": "app" for the app itself, otherwise the LockKeeper.
        const wchar_t* task = kLockKeeperTask;
        if (const auto* args = std::get_if<flutter::EncodableMap>(call.arguments())) {
          auto it = args->find(flutter::EncodableValue("task"));
          if (it != args->end() && std::get_if<std::string>(&it->second) &&
              std::get<std::string>(it->second) == "app") {
            task = kAppTask;
          }
        }
        result->Success(flutter::EncodableValue(Packaged() ? Run(m, task) : "unpackaged"));
      });
}
