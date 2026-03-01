### 1. 程序报告“找不到配置文件”或“xxxxx需要配置xxxxx API密钥。”我该如何解决？

这是一个常见的设置问题。可能发生这种情况的原因有几个：

1. **文件位置或名称不正确：**

* 程序需要一个名为`config.toml`的配置文件。确保您没有意外地将其命名为`config.toml.txt`。
* 此文件必须放置在`config`文件夹内。工作目录的正确结构应为：
  ```
  /── config/
  │   └── config.toml
  └── krillinai.exe（您的可执行文件）
  ```
* **对于Windows用户：** 建议将整个软件目录放在不在C:驱动器上的文件夹中，以避免潜在的权限问题。

2. **API密钥配置不完整：**

* 应用程序需要为大型语言模型（用于翻译）、语音服务（用于转录和语音合成）和tts服务分别配置。
* 即使您使用OpenAI进行所有操作，您也必须在`config.toml`文件的不同部分填写密钥。查找`llm`部分、`transcribe`部分、`tts`部分，并填写相应的API密钥和其他所需信息。

### 2. 我收到“TypeError: Failed to fetch”或 YouTube 链接无法下载。该怎么办？

**“Failed to fetch”通常有两种情况：**

1. **浏览器无法连接后端**：请确认 KrillinAI 服务器已启动（运行 `krillin-server.exe` 或 `run-server.bat`），并在浏览器访问 http://127.0.0.1:8888。
2. **YouTube 无法访问**：在中国大陆等地区，访问 YouTube 需要代理。请在 `config.toml` 的 `[app]` 中配置 `proxy`，例如：
   ```toml
   proxy = "http://127.0.0.1:7890"
   ```
   将地址改为您本机代理软件的端口。

### 3. 我收到包含“yt-dlp错误”或“B站下载失败”的错误。该怎么办？

此错误指向视频下载器的问题，通常与您的网络或下载器的版本有关。

* **B站 HTTP 412 错误：** B站会检测自动化访问，返回 412 时通常需要**登录态 Cookie**。请：
  1. 在浏览器登录 bilibili.com；
  2. 使用扩展（如 Chrome 的 "Get cookies.txt LOCALLY"）导出 `cookies.txt`；
  3. 将 `cookies.txt` 放到 KrillinAI 项目根目录（与 `bin` 同级）。
* **网络：** 如果您使用代理，请确保在`config.toml`文件中的代理设置中正确配置。
* **更新`yt-dlp`：** 随软件捆绑的`yt-dlp`版本可能已过时。在项目目录运行：
  ```
  .\bin\yt-dlp.exe -U
  ```
  
  （Windows 示例；Linux/macOS 将路径改为 `./bin/yt-dlp`）。

### 4. 视频和音频是分开的两个文件，怎么处理？

KrillinAI 支持**视频+音频分开**的输入方式：

1. 选择「本地上传」；
2. 在文件选择框中**同时选择两个文件**：一个视频文件（.mp4、.mkv 等）和一个音频文件（.mp3、.wav 等）；
3. 上传后直接执行任务即可。

程序会根据文件扩展名自动识别哪个是视频、哪个是音频，用音频做转录/翻译，用视频做字幕嵌入或 TTS 合成。

### 5. 最终视频中的字幕乱码或显示为方块，特别是在Linux上。

这几乎总是由于系统缺少字体，特别是那些支持中文字符的字体。要解决此问题，您需要安装所需的字体。

1. 下载所需的字体，例如[Microsoft YaHei](https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/%E5%AD%97%E4%BD%93/msyh.ttc)和[Microsoft YaHei Bold](https://modelscope.cn/models/Maranello/KrillinAI_dependency_cn/resolve/master/%E5%AD%97%E4%BD%93/msyhbd.ttc)。
2. 创建一个新的字体目录：`sudo mkdir -p /usr/share/fonts/msyh`。
3. 将下载的`.ttc`字体文件复制到此新目录中。
4. 执行以下命令以重建字体缓存：
    ```
    cd /usr/share/fonts/msyh
    sudo mkfontscale
    sudo mkfontdir
    sudo fc-cache -fv
    ```

### 6. 在macOS上，应用程序无法启动并显示“KrillinAI已损坏，无法打开”的错误。

这是由于macOS的安全功能Gatekeeper，限制了来自未识别开发者的应用程序。要解决此问题，您必须手动删除隔离属性。

1. 打开**终端**应用程序。
2. 输入命令`xattr -cr`后跟一个空格，然后将`KrillinAI.app`文件从Finder窗口拖到终端中。命令看起来像这样：
    ```
    xattr -cr /Applications/KrillinAI.app
    ```
3. 按Enter键。您现在应该能够打开该应用程序。

### 7. 在处理过程中，我收到类似`ffmpeg错误`、`audioToSrt错误`或`退出状态1`的错误。

这些错误通常指向依赖项或系统资源的问题。

* **`ffmpeg错误`：** 这表明`ffmpeg`未安装或无法从系统的PATH访问。确保您安装了完整的官方版本的`ffmpeg`，并且其位置已添加到系统的环境变量中。
* **`audioToSrt错误`或`退出状态1`：** 此错误发生在转录阶段（音频转文本）。常见原因包括：
  * **模型问题：** 本地转录模型（例如`fasterwhisper`）未能加载或在下载过程中损坏。
  * **内存不足（RAM）：** 运行本地模型资源密集。如果您的机器内存不足，操作系统可能会终止该进程，从而导致错误。
  * **网络故障：** 如果您使用在线转录服务（如OpenAI的Whisper API），这表明您的网络连接存在问题或API密钥无效。

### 8. 进度条没有移动。程序是冻结了吗？

不是的，只要您没有看到错误消息，程序就正在工作。进度条仅在主要任务（如转录或视频编码）完全完成后更新。这些任务可能非常耗时，导致进度条长时间暂停。请耐心等待任务完成。

### 9. 我的NVIDIA 5000系列GPU不支持`fasterwhisper`。我该怎么办？

观察到`fasterwhisper`模型可能无法与NVIDIA 5000系列GPU正常工作（截至2025年中期）。您有几个替代方案进行转录：

1. **使用基于云的模型：** 在`config.toml`文件中将`transcribe.provider.name`设置为`openai`或`aliyun`。然后，填写相应的API密钥和配置详细信息。这将使用云提供商的Whisper模型，而不是本地模型。
2. **使用其他本地模型：** 您可以尝试其他本地转录模型，例如原始的`whisper.cpp`。

### 10. 如何找到并填写文本转语音的正确语音/音调代码？

可用的语音及其对应的代码由您使用的语音服务提供商定义。请参考他们的官方文档。

* **OpenAI TTS：** [文档](https://platform.openai.com/docs/guides/text-to-speech/api-reference)（查看`voice`选项）。
* **阿里云：** [文档](https://help.aliyun.com/zh/isi/developer-reference/overview-of-speech-synthesis)（查看音调列表中的`voice`参数）。

### 11. 我如何使用本地大型语言模型（LLM），例如在Ollama上运行的模型进行翻译？

是的，您可以配置KrillinAI使用任何提供OpenAI兼容API端点的本地LLM。

1. **启动您的本地LLM：** 确保您的本地服务（例如，运行Llama3的Ollama）处于活动状态并可访问。
2. **编辑`config.toml`：** 在大型语言模型（翻译器）部分：

* 将提供者`name`（或`type`）设置为`"openai"`。
* 将`api_key`设置为任何随机字符串（例如，`"ollama"`），因为本地调用不需要它。
* 将`base_url`设置为您本地模型的API端点。对于Ollama，这通常是`http://localhost:11434/v1`。
* 将`model`设置为您提供的模型名称，例如`"llama3"`。

### 12. 我可以自定义最终视频中的字幕样式（字体、大小、颜色）吗？

不可以。目前，KrillinAI生成**硬编码字幕**，这意味着它们直接嵌入到视频帧中。该应用程序**不提供自定义字幕样式的选项**；它使用预设样式。

对于高级自定义，推荐的解决方法是：

1. 使用KrillinAI生成翻译后的`.srt`字幕文件。
2. 将您的原始视频和此`.srt`文件导入专业视频编辑器（例如，Premiere Pro、Final Cut Pro、DaVinci Resolve），以在渲染之前应用自定义样式。

### 13. 我已经有一个翻译后的`.srt`文件。KrillinAI可以仅使用它进行配音吗？

不可以，目前不支持此功能。该应用程序运行从转录到最终视频生成的完整流程。