# 从源码构建 KrillinAI

## 环境要求

- Go 1.21+
- （仅桌面版）Windows 需安装 **MinGW-w64**（GCC），用于编译 OpenGL 相关依赖

## 服务器版（Web UI）

```powershell
cd E:\KrillinAI-Kay
go mod tidy
go build -o bin\krillin-server.exe ./cmd/server
.\bin\krillin-server.exe
```

或直接双击项目根目录下的 `run-server.bat`，会自动检查并创建 `config/config.toml` 后启动。

启动后访问：http://127.0.0.1:8888

## 桌面版（Fyne GUI）

桌面版依赖 CGO 和 C 编译器。在 Windows 上需要先安装 **MinGW-w64**：

### 安装 MinGW-w64（Windows）

1. **方式一：MSYS2（推荐）**
   - 下载安装 [MSYS2](https://www.msys2.org/)
   - 打开 MSYS2 终端，执行：
     ```bash
     pacman -S mingw-w64-ucrt-x86_64-gcc
     ```
   - 将 `C:\msys64\ucrt64\bin` 添加到系统 PATH

2. **方式二：TDM-GCC**
   - 下载 [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)
   - 安装时勾选「Add to PATH」

3. **验证安装**
   ```powershell
   gcc --version
   ```

### 编译桌面版

```powershell
cd E:\KrillinAI-Kay
go mod tidy
go build -o bin\krillin-desktop.exe ./cmd/desktop
.\bin\krillin-desktop.exe
```

若仍报错 `build constraints exclude all Go files`，请确认：
- `gcc` 已在 PATH 中
- 未设置 `CGO_ENABLED=0`

## 配置文件

首次运行前，若 `config/config.toml` 不存在，可复制示例配置：

```powershell
copy config\config-example.toml config\config.toml
```

然后根据注释编辑 `config/config.toml`，或在 Web/桌面界面中配置。
