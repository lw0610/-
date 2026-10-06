# Third-Party Skills

该目录用于承载构建阶段同步进来的第三方技能包默认资源。

当前由根脚本 `build.sh` 负责把 `tools/data_downloader` 生成的 `googlemapd-cli-agent` 同步到这里，随后由 `carto_agent` sidecar 在编译期内嵌，并在运行时落盘到 `autocarto/config/third_party_skills/`。
