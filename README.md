## Windows 下交叉编译 Linux 64 位版本

如果您在 Windows 系统下开发，但需要为 Linux 64 位系统（amd64 架构）编译可执行文件，可以使用 Go 的交叉编译功能。以下是详细步骤。

### 前提条件
- Go 版本：1.18 或更高（确保已安装并配置好环境变量，如 GOROOT 和 GOPATH）。
- 项目依赖：确保已运行 `go mod download` 安装所有依赖。
- 无需额外工具：Go 默认支持 Windows 到 Linux/amd64 的交叉编译。

### 步骤
1. **打开命令提示符或 PowerShell**：
   - 按 Win + R，输入 `cmd` 或 `powershell`，进入项目目录：
     ```bash
     cd c:\Users\Administrator\Downloads\go双向机器人
     ```

2. **安装依赖**（如果尚未完成）：
   ```bash
   go mod download


   - - 说明 ：
  - GOOS=linux ：指定目标操作系统为 Linux。
  - GOARCH=amd64 ：指定目标架构为 64 位（x86_64）。
  - 输出文件 robot 将是 Linux 可执行文件（无 .exe 后缀）。
  - 如果项目有 CGO 依赖（例如涉及 C 库），可能需要启用 CGO 并安装 MinGW 等工具：添加 set CGO_ENABLED=1 ，并确保 MinGW 已安装。
- 2.
验证编译结果 ：

- 编译后，您会看到 robot 文件生成在当前目录。
- 将此文件传输到 Linux 系统（例如使用 SCP 或文件共享），然后在 Linux 下运行：


此双向机器人可以广播，拉黑用户，自定义添加欢迎语，按钮等功能，演示地址：https://t.me/abcdefgsxbot