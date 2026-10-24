# AutoCarto AI

AutoCarto 把专业智能体与 QGIS 操作连接起来。角色通过**工具包**获得能力，通过 **FLOW.md 流程包**按步骤协作，通过运行输入选择数据，用流程自带资源和**模板与样式库**复用文件说明、符号、样式和版式。

当前开发副本是 `enio2`；原 `Eino` 保留。[v3 改进](docs/refactor-v3/README.md) 在 v2 资源清单、唯一 Go 解析器和 Python 句柄消费基础上，增加非阻塞计划、模板与样式库、从工程保存与数据输入记忆。流程当前采用保存后直接更新预览，不再提供重新编译或自动启动后台模型分析。实施和测试以 [v3 验证记录](docs/refactor-v3/evidence/) 为准；[v2 记录](docs/refactor-v2/evidence/) 保留历史基线。离线制图采用真实控制器和 QGIS，角色动作是固定夹具，不能用它证明在线模型规划或视觉审查质量。

## 使用入口

已构建的桌面程序按操作系统方式打开，程序负责启动后端 sidecar。启动 QGIS 并启用 AutoCarto 插件后，在应用中确认工程连接，配置模型，再选择智能体方案。

在“工具中心”浏览能力；在方案中为角色选择工具包、启用流程。在“流程中心”导入、复制或编辑流程，点击“保存”后直接显示最新步骤预览；同一行的“试运行”进入对话页填写运行输入。没有合适流程时，角色仍可自由委派协作。Plan 用于只读规划和预览，Build 才执行工具。

- [项目介绍](docs/项目介绍.md)
- [流程包编写指南](docs/流程包编写指南.md)
- [模板与样式指南](docs/资源制作指南.md)
- [新增工具与工具包](docs/新增工具与工具包.md)
- 完整示例：[人口密度](docs/refactor/acceptance-flows/province-density/FLOW.md)、[考察路线](docs/refactor/acceptance-flows/survey-route/FLOW.md)，数据均标注其真实来源或合成用途。

## 从源码启动

需要 QGIS、Go（仓库要求 `go 1.24.0`，声明工具链 `go1.24.11`）、Node/npm、Rust/Cargo 和所在平台的 Tauri 构建依赖。真实 Python 制图测试必须使用能够导入 QGIS 的解释器。脚本预检会指出缺少的组件；不会自动安装系统依赖。Ubuntu 的显式环境准备脚本为 `bootstrap-ubuntu.sh`，它会安装或下载依赖，使用前先检查其范围。

在项目根目录执行：

```sh
./build.sh help
./build.sh deps
./build.sh dev
```

Windows 对应 `build.bat help`、`build.bat deps`、`build.bat dev`。

`deps` 根据锁文件准备前端依赖；`dev` 构建 Go sidecar、**替换当前 QGIS profile 中的 `autocarto` 插件目录**，再启动 Tauri 和本地前端（开发地址 `http://localhost:3000`）。先保存 QGIS 工程，确认插件部署目标，重新加载插件后连接。仅在 `autocarto-web/` 执行 `npm run dev` 只启动网页，不负责完整系统和插件部署。

准备好上述开发工具链并安装前端依赖后，也可直接沿用桌面开发入口：

```sh
cd autocarto-web
npx tauri dev
```

它会自动构建当前 Go 后端，再启动 Tauri 和前端，无须预先准备 sidecar 或额外执行 `./build.sh go`。后端构建失败会停止桌面构建，避免启动旧二进制。`./build.sh dev` 还负责完整的 QGIS 插件部署；macOS 开发模式从应用新建 QGIS 实例时会同步已安装的开发插件，但仍需存在可用的 QGIS profile 与 AutoCarto 插件。不要保留指向旧 `Eino` 的 `AUTOCARTO_*` 环境变量，否则可能读到旧资产或运行目录。

其他入口：

| 命令 | 实际作用 |
| --- | --- |
| `./build.sh go` | 仅构建 sidecar，保留运行中的前端缓存 |
| `./build.sh check` | 隔离运行目录，执行 Go、Web、Python、E2E、Next 和 Rust 门禁；不部署插件 |
| `./build.sh build` | 构建桌面应用并部署 QGIS 插件，省略命令时也会执行此操作 |
| `./build.sh plugin` | 打包 QGIS 插件 ZIP |
| `./build.sh dist` | 生成当前平台的桌面与插件发行产物，整理到 `dist/` 并执行发行包 smoke |

平台专属的 `app`、`dmg`、`deb`、`appimage` 和已有发行包检查以 `./build.sh help` 为准。以上是现有命令说明，不表示本次验收安装或部署了发行包。

## 项目内运行目录与路径

默认运行目录为**固定项目根下的 `autocarto_data/`**，相对配置路径按项目根解析，不按进程启动时的工作目录解析。

```text
autocarto_data/
├── config/                   应用设置、智能体方案、加密凭据和本机输入记忆
├── data/
│   ├── logs/                 后端日志
│   ├── sessions/             各工程的会话、步骤运行和成果
│   └── flow_cache/           执行计划与历史兼容缓存
├── flows/                    我的流程及包内资源
└── resources/                用户版式、样式和样式集
```

内置资产位于 `autocarto/{tools,skills,flows,resources}/system/` 对应目录，工具包目录仍叫 `skills`。工具内部文件在工具域 `assets/`；场景内容随流程 `resources/`；公共库只保存版式、样式和样式集。内置公开项为国标底图样式、四角版式、统计版式三项；默认单色路线已归路线工具资产。内置流程只读，修改前复制为自己的流程。

模板与样式可以自动识别导入，也可以从已连接的 QGIS 工程保存版面、单图层样式或样式集，系统自动生成资源清单和编号。同名用户项不会自动优先：已有 `flow.lock` 按锁定编号解析，否则同类型同名候选需要选择；明确 `overrides` 才覆盖内置项。

用户在运行输入中选择文件、文件夹或工程图层，选择后即时检查。文件与文件夹默认记住上次成功启动时的值，可以清除或声明“不记住”。固定多文件流程由作者准备包内数据清单并关联文件夹输入；换数据存放目录只需重新选择。新增图层或修改清单文件名仍需作者编辑清单，目前没有图形化逐行清单编辑器。

流程导出默认携带版式/样式/样式集依赖，导入保留有差异的随包副本，不静默写入用户库。数据清单中的外部文件由接收方提供并在输入表单中选择，本机输入记忆不随包传播。

需要显式定位时，在启动进程前设置：

```sh
export AUTOCARTO_PROJECT_ROOT="/项目的绝对路径/enio2"
export AUTOCARTO_HOME="autocarto_data"
export AUTOCARTO_ASSETS_ROOT="autocarto"
```

`AUTOCARTO_PROJECT_ROOT` 必须是绝对路径；另外两项可相对于它。项目根通常从显式环境、资产、可执行文件或源码位置定位；桌面便携应用按自身位置定位，必要时明确覆盖。本机输入记忆使用绝对路径，配置中的相对运行路径从固定项目根解析；清单文件相对所关联的输入文件夹，没有关联时相对清单目录。运行目录必须可写，不要沿用旧副本的环境变量。

流程计划包含输入、步骤、角色、条件和资源。打开、保存、复制和导入流程后直接生成预览，不启动后台模型分析。编辑器中尚未保存的修改不影响右侧预览，保存后显示最新内容；数据选择和文件变化会重新链接资源。必要的格式、角色、资源及运行输入校验仍保留，实际执行时按角色配置调用模型和工具。

每次运行在会话目录保存 `flows/<运行编号>/run.json`，记录当时的执行计划和实际产出。成果以工具返回路径为准，通常归档到会话 `exports/`。运行快照不备份外部数据字节，不要把缓存计划当作流程配置编辑。

## 备份与验证

迁移、批量编辑方案或更换运行目录前，先停止相关服务，再完整备份运行目录，包含隐藏文件。加密配置与其密钥必须一并保留；备份不要提交到仓库或分享到日志。只备份方案 YAML 不能替代完整配置备份。已有迁移的保留范围和原始备份见 [T51 记录](docs/refactor/evidence/T51.md)。

日常门禁为 `./build.sh check`，报告位于 `.sisyphus/evidence/`；真实 QGIS 与黄金样例的解释器、外部数据要求及明确跳过规则见 [黄金回归说明](autocarto/tests/golden/README.md)。只读开发验收成果位于 `autocarto/tests/golden/.runs/T53/`，不会写入工作运行目录。

[上一版 T53](docs/refactor/evidence/T53.md) 保存当时通过的完整门禁与跳过范围；不把它作为当前修改后的通过证明。当前测试以 [refactor-v3 证据](docs/refactor-v3/evidence/) 为准，跳过项不算通过，桌面编译也不等同于已安装发行包或实时 QGIS 界面联调。
