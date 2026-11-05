# 项目上下文

## 项目概况

NekoBox for Android 是一个单模块 Android 应用，根项目名为 NB4A，应用模块为 app。应用元数据来自 nb4a.properties：包名 moe.nb4a，版本 1.4.2，版本号 46。

## 已验证环境

- 操作系统：Windows 11 amd64。
- JDK：Eclipse Temurin 21.0.12.1。
- Gradle Wrapper：8.10.2，分发包已成功下载。
- Android Gradle Plugin：8.8.1，定义在 buildSrc/build.gradle.kts。
- Kotlin Gradle Plugin：2.0.21，定义在 buildSrc/build.gradle.kts。
- Android 配置：compileSdk 35、targetSdk 35、minSdk 21、buildToolsVersion 35.0.1。
- Android SDK、Build Tools、Windows NDK、模拟器和 Android 35 x86_64 系统镜像已安装到 H:/hack/android/Sdk。
- gradlew.bat tasks --all --console=plain：通过。
- go.exe：可用，版本 go1.26.4；libcore/go.mod 声明 go 1.23.1 和 toolchain go1.23.6。
- bash.exe、wsl.exe：命令入口存在，但 bash.exe 实际转发到 WSL；bash.exe -c 'uname -s' 实测因 WSL 运行时错误退出。原生构建脚本使用 Bash，并固定使用 Linux NDK 工具链目录。
- Gradle 已通过 local.properties 使用 H:/hack/android/Sdk；当前编译已进入 Kotlin 阶段。

## 当前缺口

- ANDROID_HOME 和 ANDROID_SDK_ROOT 仍未设置；项目通过被忽略的 local.properties 指向 H:/hack/android/Sdk。
- adb、sdkmanager、emulator、avdmanager 已安装在 H:/hack/android/Sdk，但尚未加入当前 PATH。
- local.properties 已写入 sdk.dir 和 ndk.dir，仅用于本机 Gradle。
- app/libs/libcore.aar 不存在；app/build.gradle.kts 会从 app/libs 加载本地库。
- libcore/go.mod 的本地替换目录 libneko 和 sing-box 当前未发现。
- gomobile-matsuri 和 gobind-matsuri 二进制当前未在 PATH 中发现；gomobile 源码已下载到 H:/hack/android/native/gomobile。
- WSL Linux 运行时当前不可用，无法直接执行 libcore 的 Linux 原生构建脚本。
- 未发现 app/src/test 和 app/src/androidTest 测试源目录。Gradle 任务仍提供单元测试和连接设备测试入口。

## 原生库构建链

release.yml 的构建链先执行 bash ./run lib core，再执行 Gradle OSS 构建。libcore/build.sh 会生成 app/libs/libcore.aar。原生脚本要求 Android NDK 25.0.8775105，并使用 Linux NDK 目录 linux-x86_64；Windows 本地构建应使用可提供该目录的 Linux 兼容环境，或使用已生成的 AAR。

CI 工作流使用 Go 版本范围 ^1.25；仓库 libcore/go.mod 自身声明 toolchain go1.23.6。实际原生构建前需确认 Go、gomobile-matsuri、gobind-matsuri 以及 libneko、sing-box 本地替换目录完整。

## 工作约定

- 默认构建变体：ossDebug。
- 默认测试：app:testOssDebugUnitTest。当前没有测试源时，任务可能以 NO-SOURCE 结束。
- 默认编译/打包：app:assembleOssDebug。
- SDK 配置后已重新执行 app:compileOssDebugKotlin 和 app:testOssDebugUnitTest；编译已越过 SDK 检查，但因缺少 app/libs/libcore.aar 产生 libcore 未解析错误，测试尚未开始。
- 设备测试：app:connectedOssDebugAndroidTest，要求 Android SDK、adb 和已连接设备或模拟器。
- 原生准备：bash ./run lib core。该命令在依赖、NDK 和 Linux 兼容 shell 未就绪时应先补齐前置条件。
- 签名密码、密钥和本机 SDK 路径具体值不写入知识库。

## 证据来源

- settings.gradle.kts、build.gradle.kts、app/build.gradle.kts。
- buildSrc/build.gradle.kts、buildSrc/src/main/kotlin/Helpers.kt。
- gradle/wrapper/gradle-wrapper.properties、gradle.properties、nb4a.properties。
- run、buildScript/init/env.sh、buildScript/init/env_ndk.sh、libcore/build.sh、libcore/go.mod。
- .github/workflows/release.yml、app/src/main/java/moe/matsuri/nb4a/NativeInterface.kt。
- 本机 Gradle Wrapper 任务枚举和工具探测结果。
