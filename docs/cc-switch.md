# 使用 CC Switch 接入 coococode 中转

CC Switch 是一个开源桌面工具（[GitHub](https://github.com/farion1231/cc-switch)），用于管理和一键切换 Claude Code / Codex 的 API 供应商配置，不用手动编辑配置文件。本文介绍如何用它把 Claude Code / Codex 接到 coococode 中转。

## 1. 下载安装 CC Switch

下载地址：https://github.com/farion1231/cc-switch/releases/latest

| 系统 | 安装方式 |
|---|---|
| Windows | 下载 `.msi` 安装包，双击安装 |
| macOS | `brew tap farion1231/ccswitch` 然后 `brew install --cask cc-switch` |
| Linux (Debian/Ubuntu) | 下载 `.deb` 包后 `sudo dpkg -i cc-switch_x.x.x_amd64.deb`（文件名中的版本号替换为实际下载的版本） |

## 2. 准备 coococode 的 API Key

1. 登录 coococode 控制台：<https://coococode.com>
2. 左侧菜单进入「API 密钥」页面
3. 新建一个密钥并复制（`sk-` 开头），后续配置都要用它

## 3. 配置 Claude Code

1. 打开 CC Switch，顶部应用切换栏选择 **Claude Code**
2. 点击右上角 **+** 添加供应商，选择自定义添加（coococode 无内置预设，需手动填写）
3. 填写配置：
   - **名称**：`coococode`
   - **API 地址**：`https://coococode.com`
   - **API Key**：第 2 步复制的密钥
4. 保存后，在主界面点击该供应商右侧的「启用」，显示「使用中」即切换成功
5. 验证：终端运行 `claude`，能正常对话即接入完成

> 不用 CC Switch 时，等价的环境变量配置：
>
> ```bash
> export ANTHROPIC_BASE_URL=https://coococode.com
> export ANTHROPIC_AUTH_TOKEN=sk-你的密钥
> ```

## 4. 配置 Codex

1. CC Switch 顶部应用切换栏选择 **Codex**，点击右上角 **+** 添加供应商
2. 填写配置：
   - **名称**：`coococode`
   - **API 地址**：`https://coococode.com/v1`（注意带 `/v1`）
   - **API Key**：同一个密钥即可
3. 启用后，终端运行 `codex`，能正常对话即接入完成

## 5. 常见问题

- **提示余额不足**：平台充值由管理员人工处理，联系管理员充值，见 [FAQ](./FAQ.md)。
- **401 / Key 无效**：检查密钥是否复制完整、是否已在控制台被删除或禁用。
- **模型不存在 / 404**：确认请求的模型在平台可用清单内，可在控制台模型广场查看。
- **Claude Code 首次启动卡在确认页**：CC Switch 设置 → 通用 → 开启「跳过 Claude Code 初次安装确认」。
