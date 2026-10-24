#ifndef RUNNER_STARTUP_TASK_H_
#define RUNNER_STARTUP_TASK_H_

#include <flutter/binary_messenger.h>

// Channel "encryptlock/startup": "Switch on by itself when Windows starts"
// for the Microsoft Store package (MSIX), whose startup task (declared in
// the package manifest, TaskId "LockKeeperStart") runs lockkeeper.exe.
// Methods: "state" (unpackaged, enabled, disabled, disabledByUser,
// disabledByPolicy, enabledByPolicy), "enable", "disable" (both return the
// new state). Outside the package the app keeps its Startup-folder shortcut.
void RegisterStartupTaskChannel(flutter::BinaryMessenger* messenger);

#endif  // RUNNER_STARTUP_TASK_H_
