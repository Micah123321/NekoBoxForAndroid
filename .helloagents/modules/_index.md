# 模块索引

| 模块 | 类型 | 入口 | 说明 |
|---|---|---|---|
| app | Android application | app/build.gradle.kts | 主应用、资源、变体与测试任务 |
| buildSrc | Gradle build logic | buildSrc/src/main/kotlin/Helpers.kt | Android SDK、版本、变体、签名和 lint 配置 |
| libcore | Go native library | libcore/build.sh | 生成 app/libs/libcore.aar，依赖 Go、NDK 和本地替换目录 |
| root | Gradle root project | settings.gradle.kts | 项目名 NB4A，包含 app 模块 |
