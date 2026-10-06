# GoogleMapD CLI Agent Skill

可复用的 CLI Agent Skill 包，让 AI Agent 通过命令行操控 GoogleMapD 进行瓦片估算、下载、恢复、合并和矢量数据工作流。

## 目录结构

```
third_party_skills/googlemapd-cli-agent/
├── SKILL.md                      # Skill 主入口（Agent 读取此文件）
├── README.md                     # 本文件
├── bin/                          # make build 自动生成的二进制（gitignore）
│   └── googlemapd
├── configs/                      # make build 自动复制的配置（gitignore）
│   └── config.example.yaml
├── agents/
│   ├── openai.yaml               # OpenAI Codex 接入模板
│   └── claude.md                 # Claude Code 接入模板
├── references/
│   ├── tooling.md                # 工具参数参考
│   └── workflows.md              # 端到端工作流
└── scripts/
    └── check_env.sh              # 环境预检脚本
```

## 快速开始

```bash
# 1. 在 AutoCartoAI 中构建并同步第三方 skill 包
./build.sh go

# 2. 验证环境
bash carto_agent/defaults/third_party_skills/googlemapd-cli-agent/scripts/check_env.sh

# 3. 单独维护下载器仓库时，仍可直接本地构建
make build

# 4. 验证 skill 结构（可选）
make validate-skill
```

在 AutoCartoAI 内，`build.sh` 会先把 `tools/data_downloader/skills/googlemapd-cli-agent/` 的静态资源同步到 `carto_agent/defaults/third_party_skills/googlemapd-cli-agent/`，再把编译后的 `googlemapd` 二进制和示例配置嵌入进去，随后编译 `carto_agent` sidecar。

## 支持的 Agent 平台

| 平台 | 接入方式 | 配置文件 |
|------|----------|----------|
| Claude Code | CLAUDE.md 引用 SKILL.md | `agents/claude.md` |
| OpenAI Codex | Agent 配置 | `agents/openai.yaml` |
| 通用 Agent | 手动注入 SKILL.md 为上下文 | SKILL.md + references/ |

详细接入指南见 `docs/agent-skill-quickstart.md`。
