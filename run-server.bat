@echo off
chcp 65001 >nul
cd /d "%~dp0"

if not exist "config\config.toml" (
    echo 正在从 config-example.toml 创建 config.toml ...
    copy "config\config-example.toml" "config\config.toml"
)

echo 启动 KrillinAI 服务器...
echo 启动后请在浏览器访问: http://127.0.0.1:8888
echo.
.\bin\krillin-server.exe
pause
