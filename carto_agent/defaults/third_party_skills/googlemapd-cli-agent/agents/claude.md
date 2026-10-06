# Claude Code 接入模板

## 方式一：在项目 CLAUDE.md 中引用（推荐）

在项目根目录的 `CLAUDE.md` 中添加：

```markdown
## GoogleMapD CLI Skill

操作 GoogleMapD 时，参考以下文档：
- 主入口：`third_party_skills/googlemapd-cli-agent/SKILL.md`
- 工具参数：`third_party_skills/googlemapd-cli-agent/references/tooling.md`
- 工作流：`third_party_skills/googlemapd-cli-agent/references/workflows.md`

规则：
- 优先使用 V2 命令（`tiles ...`、`vector ...`）
- 始终加 `--json` 获取机器可读输出
- 大范围下载前先用 `tiles info --json` 估算
- 使用 skill 包内的二进制：`third_party_skills/googlemapd-cli-agent/bin/googlemapd`
```

## 方式二：作为 Custom Slash Command

在 `.claude/commands/` 目录下创建 `googlemapd.md`：

```markdown
使用 GoogleMapD CLI 完成用户请求。

参考文档：
- `third_party_skills/googlemapd-cli-agent/SKILL.md`
- `third_party_skills/googlemapd-cli-agent/references/tooling.md`

二进制路径：`third_party_skills/googlemapd-cli-agent/bin/googlemapd`
始终使用 V2 命令和 --json 标志。

用户请求：$ARGUMENTS
```

然后通过 `/googlemapd 下载北京五环内 zoom 15 的卫星瓦片` 调用。

## 验证

```bash
# 确认二进制可用
third_party_skills/googlemapd-cli-agent/bin/googlemapd version

# 运行环境检查
bash third_party_skills/googlemapd-cli-agent/scripts/check_env.sh
```
