# AutoCarto 运行数据归档

此目录保存 2026-10-29 的运行配置、智能体方案、历史会话和制图成果的脱敏副本。本地原件未修改。

## 包含内容

- `config/`：运行配置、当前智能体方案及其附属技能。
- `flows/`：自定义流程。
- `data/symbol/`：自定义 SVG 符号。
- `data/reference_inputs/`、`data/acquired/`、`data/boundaries/`：任务输入及取得的数据。
- `data/knowledge_reviews/`：知识抽取记录及图文素材。
- `data/sessions/`：历史对话、会话快照、任务计划、中间数据，以及各会话 `exports/`、`output/` 中的成图成果。

## 脱敏与排除

本机用户目录统一替换为 `/home/shared-user`；配置中的最近工程及机器相关运行路径已清理，历史文本已检查凭据字段和常见令牌格式。导出 PDF 的本机作者信息，以及图文素材中发现的个人作者和 GPS 元数据已移除，地图页面内容与图片像素保留。

没有上传 `.master_key`、`secrets.enc`、认证数据库、QGIS 运行配置目录、检查点数据库、日志、临时文件和历史配置备份。根目录原有 `exports/` 是执行日志导出，不属于地图成果，未纳入本归档。

## 大型影像

两幅超过 Git 单文件限制的影像单独存放在 [runtime-data-20261029 Release](https://github.com/lw0610/-/releases/tag/runtime-data-20261029)。`large-files.json` 列出了原相对路径、文件大小、校验值和下载地址；其中 `path` 相对于仓库中的 `autocarto_data/` 目录。如需完整文件结构，下载后放回对应路径。

## 使用说明

这是一份可查阅的运行数据归档，不含账号登录状态。使用时需要在系统设置中重新配置模型/API 密钥、QGIS 路径和必要的 GEE 环境，并选择自己的工程。历史记录中的 `/home/shared-user` 是匿名路径，应按实际机器重新定位文件，不能直接当作有效本机路径。
