import React from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import IntegrationsConfigWorkspace, { DEFAULT_INTEGRATIONS } from "@/components/settings/IntegrationsConfigWorkspace";

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), batchSetConfig: vi.fn(), checkIntegration: vi.fn() }));

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, default: mocks };
});
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() } }));

describe("online integration settings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getConfig.mockResolvedValue({ integrations: {
      ...DEFAULT_INTEGRATIONS,
      gee: { ...DEFAULT_INTEGRATIONS.gee, project: "example-project", enabled: true },
      browser: { ...DEFAULT_INTEGRATIONS.browser, enabled: true },
    } });
    mocks.batchSetConfig.mockResolvedValue(undefined);
    mocks.checkIntegration.mockResolvedValue({ service: "gee", success: true, result: { initialized: true } });
  });

  it("loads saved values without running checks or saving defaults", async () => {
    render(<IntegrationsConfigWorkspace />);
    expect(await screen.findByDisplayValue("example-project")).toBeInTheDocument();
    expect(screen.getByLabelText("启动参数（每行一个）")).toHaveValue("-y\n@playwright/mcp@0.0.83");
    expect(screen.getByLabelText("GEE 无输出超时（秒）")).toHaveValue(300);
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
    expect(mocks.batchSetConfig).not.toHaveBeenCalled();
  });

  it("saves the complete integrations object through the existing config mutation API", async () => {
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.change(screen.getByLabelText("Google Cloud 项目 ID"), { target: { value: "another-project" } });
    fireEvent.change(screen.getByLabelText("启动参数（每行一个）"), { target: { value: "-y\n@playwright/mcp@0.0.83\n--timeout-navigation\n60000\n" } });
    fireEvent.click(screen.getByLabelText("启用网页读取与下载"));
    expect(screen.getByRole("button", { name: "保存并检查 GEE 运行环境" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "保存在线数据配置" }));
    await waitFor(() => expect(mocks.batchSetConfig).toHaveBeenCalledWith([{ path: "integrations", value: {
      ...DEFAULT_INTEGRATIONS,
      gee: { ...DEFAULT_INTEGRATIONS.gee, enabled: true, project: "another-project" },
      browser: { ...DEFAULT_INTEGRATIONS.browser, enabled: true, args: ["-y", "@playwright/mcp@0.0.83", "--timeout-navigation", "60000"] },
      web: { ...DEFAULT_INTEGRATIONS.web, enabled: true },
    } }]));
    await waitFor(() => expect(screen.getByRole("button", { name: "检查 GEE 运行环境" })).toBeEnabled());
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
  });

  it("runs an explicit diagnostic against saved configuration and shows structured detail", async () => {
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查 GEE 运行环境" }));
    await waitFor(() => expect(mocks.checkIntegration).toHaveBeenCalledWith("gee", 300));
    expect(await screen.findByText("GEE 检查结果")).toBeInTheDocument();
    expect(screen.getByText(/"initialized": true/)).toBeInTheDocument();
  });

  it.each(["gee", "browser"] as const)("allows environment checks for saved, disabled %s without enabling execution", async (service) => {
    mocks.getConfig.mockResolvedValue({ integrations: DEFAULT_INTEGRATIONS });
    mocks.checkIntegration.mockResolvedValue({ service, success: true, result: { enabled: false } });
    render(<IntegrationsConfigWorkspace />);
    const name = service === "gee" ? "检查 GEE 运行环境" : "检查浏览器运行环境";
    await waitFor(() => expect(screen.getByRole("button", { name })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name }));
    await waitFor(() => expect(mocks.checkIntegration).toHaveBeenCalledWith(service, service === "gee" ? 300 : 60));
    expect(screen.getByLabelText(service === "gee" ? "启用 GEE" : "启用浏览器")).not.toBeChecked();
    expect(mocks.batchSetConfig).not.toHaveBeenCalled();
  });

  it.each(["gee", "browser"] as const)("saves all dirty settings before checking %s and prevents overlapping actions", async (service) => {
    let finishSave!: () => void;
    mocks.batchSetConfig.mockImplementationOnce(() => new Promise<void>((resolve) => { finishSave = resolve; }));
    mocks.checkIntegration.mockResolvedValue({ service, success: true, result: { command_available: true, environment: { ready: true } } });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.change(screen.getByLabelText("Google Cloud 项目 ID"), { target: { value: "new-project" } });
    fireEvent.change(screen.getByLabelText(service === "gee" ? "GEE 无输出超时（秒）" : "浏览器调用超时（秒）"), { target: { value: "90" } });
    const name = service === "gee" ? "保存并检查 GEE 运行环境" : "保存并检查浏览器运行环境";
    expect(screen.getByText(/当前修改尚未保存/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name }));
    expect(mocks.batchSetConfig).toHaveBeenCalledTimes(1);
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Google Cloud 项目 ID")).toBeDisabled();
    expect(screen.getByRole("button", { name: "正在保存配置…" })).toBeDisabled();
    const otherName = service === "gee" ? "保存并检查浏览器运行环境" : "保存并检查 GEE 运行环境";
    expect(screen.getByRole("button", { name: otherName })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: otherName }));
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
    await act(async () => { finishSave(); });
    await waitFor(() => expect(mocks.checkIntegration).toHaveBeenCalledExactlyOnceWith(service, 90));
    expect(mocks.batchSetConfig).toHaveBeenCalledWith([{ path: "integrations", value: expect.objectContaining({ gee: expect.objectContaining({ project: "new-project" }) }) }]);
    expect(screen.getByLabelText("Google Cloud 项目 ID")).toBeEnabled();
    expect(screen.queryByText(/当前修改尚未保存/)).not.toBeInTheDocument();
  });

  it.each(["gee", "browser"] as const)("keeps input and skips %s diagnostics after save failure, then allows retry", async (service) => {
    mocks.batchSetConfig.mockRejectedValueOnce(new Error("配置更新被阻止：请先停止当前任务"));
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.change(screen.getByLabelText("Google Cloud 项目 ID"), { target: { value: "retry-project" } });
    const name = service === "gee" ? "保存并检查 GEE 运行环境" : "保存并检查浏览器运行环境";
    fireEvent.click(screen.getByRole("button", { name }));
    expect(await screen.findByRole("alert")).toHaveTextContent("配置更新被阻止：请先停止当前任务");
    expect(screen.getByLabelText("Google Cloud 项目 ID")).toHaveValue("retry-project");
    expect(screen.getByText(/保存失败，当前输入已保留，本次未执行环境检查/)).toBeInTheDocument();
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.getByRole("button", { name })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name }));
    await waitFor(() => expect(mocks.checkIntegration).toHaveBeenCalledExactlyOnceWith(service, service === "gee" ? 300 : 60));
    expect(mocks.batchSetConfig).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("explains validation failure inline and does not save or check invalid settings", async () => {
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.change(screen.getByLabelText("GEE 无输出超时（秒）"), { target: { value: "3601" } });
    fireEvent.click(screen.getByRole("button", { name: "保存并检查浏览器运行环境" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("超时最长为 3600 秒。");
    await waitFor(() => expect(screen.getByRole("button", { name: "保存并检查浏览器运行环境" })).toBeEnabled());
    expect(mocks.batchSetConfig).not.toHaveBeenCalled();
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
  });

  it("identifies an older backend and disables saving and checks until a supported configuration is loaded", async () => {
    mocks.getConfig.mockResolvedValueOnce({});
    render(<IntegrationsConfigWorkspace />);
    expect(await screen.findByText(/当前后台服务尚未更新，请重新启动更新后的桌面应用；仅刷新页面不会更新后台/)).toBeInTheDocument();
    expect(screen.getByLabelText("Python 解释器")).toHaveValue("python3");
    expect(screen.getByLabelText("Python 解释器")).toBeDisabled();
    const save = screen.getByRole("button", { name: "保存在线数据配置" });
    const geeCheck = screen.getByRole("button", { name: "检查 GEE 运行环境" });
    const browserCheck = screen.getByRole("button", { name: "检查浏览器运行环境" });
    for (const button of [save, geeCheck, browserCheck]) {
      expect(button).toBeDisabled();
      fireEvent.click(button);
    }
    expect(mocks.batchSetConfig).not.toHaveBeenCalled();
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "刷新在线数据配置" }));
    expect(await screen.findByDisplayValue("example-project")).toBeEnabled();
    expect(screen.getByRole("button", { name: "检查浏览器运行环境" })).toBeEnabled();
    expect(screen.queryByText("后台服务需要更新")).not.toBeInTheDocument();
  });

  it("describes a successful browser check as finding the local command without claiming network access", async () => {
    mocks.checkIntegration.mockResolvedValue({ service: "browser", success: true, result: { command_available: true }, note: "local command only" });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查浏览器运行环境" }));
    expect(await screen.findByText("已找到启动程序；这项检查不会打开网页，实际浏览任务会验证浏览器是否能启动。")).toBeInTheDocument();
  });

  it("shows the browser command failure instead of a generic completed message", async () => {
    mocks.checkIntegration.mockResolvedValue({ service: "browser", success: true, result: { command_available: false, command_error: "找不到启动程序 /missing/npx" } });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查浏览器运行环境" }));
    expect(await screen.findByText("找不到启动程序 /missing/npx")).toBeInTheDocument();
    expect(screen.queryByText("SUCCESS")).not.toBeInTheDocument();
  });

  it("shows failed diagnostics even when the HTTP envelope succeeded", async () => {
    mocks.checkIntegration.mockResolvedValue({ service: "browser", success: false, error: "未找到 Chrome", result: { available: false } });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查浏览器运行环境" }));
    expect(await screen.findByText("未找到 Chrome")).toBeInTheDocument();
    expect(mocks.checkIntegration).toHaveBeenCalledWith("browser", 60);
  });

  it("distinguishes a completed environment check from GEE readiness", async () => {
    mocks.checkIntegration.mockResolvedValue({ service: "gee", success: true, result: { success: true, environment: { ready: false }, message: "需要安装 earthengine-api" } });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查 GEE 运行环境" }));
    expect(await screen.findByText("需要安装 earthengine-api")).toBeInTheDocument();
    expect(screen.getByText("WARNING")).toBeInTheDocument();
    expect(screen.queryByText("SUCCESS")).not.toBeInTheDocument();
  });

  it("warns when Earth Engine is ready but local download dependencies are missing", async () => {
    mocks.checkIntegration.mockResolvedValue({ service: "gee", success: true, result: {
      success: true, status: "ready_without_local_download", environment: {
        ready: true, download_ready: false, geedim_required_version: "2.0.0",
        download_dependency_error: "缺少 geedim 及 rasterio 下载依赖",
      },
    } });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查 GEE 运行环境" }));
    expect(await screen.findByText("GEE 已连接，可查询；本地影像下载尚未就绪。")).toBeInTheDocument();
    expect(screen.getByText(/需要 geedim==2.0.0；当前版本：未检测到/)).toBeInTheDocument();
    expect(screen.getByText("缺少 geedim 及 rasterio 下载依赖")).toBeInTheDocument();
    expect(screen.getByText("WARNING")).toBeInTheDocument();
    expect(screen.queryByText("SUCCESS")).not.toBeInTheDocument();
  });

  it("does not report full readiness when an older check omits downloader readiness", async () => {
    mocks.checkIntegration.mockResolvedValue({ service: "gee", success: true, result: { environment: { ready: true } } });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查 GEE 运行环境" }));
    expect(await screen.findByText("GEE 已连接，可查询；尚未确认本地下载依赖，请更新后台后重新检查。")).toBeInTheDocument();
    expect(screen.queryByText("SUCCESS")).not.toBeInTheDocument();
  });

  it("reports readiness only when queries, boundary validation and downloads are ready", async () => {
    mocks.checkIntegration.mockResolvedValue({ service: "gee", success: true, result: { environment: { ready: true, download_ready: true, region_ready: true, geedim_version: "2.0.0" } } });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查 GEE 运行环境" }));
    expect(await screen.findByText("GEE 已连接，边界处理与本地影像下载已就绪；实际任务仍需验证数据权限、网络和输出。")).toBeInTheDocument();
    expect(screen.getByText("SUCCESS")).toBeInTheDocument();
  });

  it.each([false, undefined])("does not claim complete readiness without boundary validation: %s", async (regionReady) => {
    mocks.checkIntegration.mockResolvedValue({ service: "gee", success: true, result: {
      environment: { ready: true, download_ready: true, region_ready: regionReady, region_dependency_error: "GeoPandas is missing" },
    } });
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查 GEE 运行环境" }));
    expect(await screen.findByText(regionReady === false
      ? "GEE 已连接；研究区边界读取与检查尚未就绪。"
      : "GEE 已连接；尚未确认研究区边界处理依赖，请更新后台后重新检查。")).toBeInTheDocument();
    expect(screen.queryByText("SUCCESS")).not.toBeInTheDocument();
    if (regionReady === false) expect(screen.getByText("GeoPandas is missing")).toBeInTheDocument();
  });

  it("keeps edits and explains why no diagnostic ran when standalone saving fails", async () => {
    mocks.batchSetConfig.mockRejectedValue(new Error("当前运行尚未结束"));
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.change(screen.getByLabelText("Google Cloud 项目 ID"), { target: { value: "unsaved-project" } });
    fireEvent.click(screen.getByRole("button", { name: "保存在线数据配置" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("当前运行尚未结束");
    expect(screen.getByLabelText("Google Cloud 项目 ID")).toHaveValue("unsaved-project");
    expect(screen.getByText(/保存失败，当前输入已保留，本次未执行环境检查/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "保存并检查 GEE 运行环境" })).toBeEnabled());
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
  });

  it("disables edits and checks after loading fails, and permits retry", async () => {
    mocks.getConfig.mockRejectedValueOnce(new Error("后端不可用"));
    render(<IntegrationsConfigWorkspace />);
    expect(await screen.findByRole("alert")).toHaveTextContent("后端不可用");
    expect(screen.getByLabelText("Python 解释器")).toBeDisabled();
    expect(screen.getByRole("button", { name: "保存在线数据配置" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "刷新在线数据配置" }));
    expect(await screen.findByDisplayValue("example-project")).toBeEnabled();
  });

  it("shows transport errors without reporting a successful check", async () => {
    mocks.checkIntegration.mockRejectedValue(new Error("检查超时"));
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.click(screen.getByRole("button", { name: "检查 GEE 运行环境" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("检查超时");
    expect(screen.queryByText("GEE 检查结果")).not.toBeInTheDocument();
  });

  it.each([
    ["GEE 代理 URL（可选）", "http://user:secret@example.test", "代理 URL 不能包含账号密码、查询参数或片段。"],
    ["GEE 代理 URL（可选）", "socks5://127.0.0.1:1080", "代理地址须使用 http 或 https。"],
    ["GEE 无输出超时（秒）", "3601", "超时最长为 3600 秒。"],
    ["单文件下载上限（MB）", "1025", "单文件下载上限不能超过 1024 MB。"],
    ["OSM 总时限（秒）", "0", "OSM 总时限须为 1–7200 秒的整数。"],
    ["OSM 总时限（秒）", "7201", "OSM 总时限须为 1–7200 秒的整数。"],
    ["最多节点尝试数", "0", "OSM 最多节点尝试数须为 1–8 的整数。"],
    ["最多节点尝试数", "9", "OSM 最多节点尝试数须为 1–8 的整数。"],
    ["最多节点尝试数", "1.5", "OSM 最多节点尝试数须为 1–8 的整数。"],
  ])("rejects invalid configuration in %s", async (label, value, message) => {
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.change(screen.getByLabelText(label), { target: { value } });
    fireEvent.click(screen.getByRole("button", { name: "保存在线数据配置" }));
    expect(toast.error).toHaveBeenCalledWith(message);
    expect(mocks.batchSetConfig).not.toHaveBeenCalled();
  });

  it("allows saving GEE before a project is supplied and explains the missing setup", async () => {
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    fireEvent.change(screen.getByLabelText("Google Cloud 项目 ID"), { target: { value: "" } });
    expect(screen.getByText(/尚未填写 Google Cloud 项目 ID/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "保存在线数据配置" }));
    await waitFor(() => expect(mocks.batchSetConfig).toHaveBeenCalled());
  });
  it("saves OSM preferences before an explicit check without a page-load network request", async () => {
    render(<IntegrationsConfigWorkspace />);
    await screen.findByDisplayValue("example-project");
    expect(screen.getByLabelText("启用 OSM")).not.toBeChecked();
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
    fireEvent.click(screen.getByLabelText("启用 OSM"));
    fireEvent.change(screen.getByLabelText("Overpass 全量节点（可选，每行一个）"), { target: { value: "https://example.test/api/interpreter" } });
    mocks.checkIntegration.mockResolvedValue({ service: "osm", success: true, result: { endpoints: [{ available: true }] } });
    fireEvent.click(screen.getByRole("button", { name: "保存并检查 OSM 服务" }));
    await waitFor(() => expect(mocks.checkIntegration).toHaveBeenCalledWith("osm", 120));
    expect(mocks.batchSetConfig.mock.calls[0][0][0].value.osm).toEqual({ ...DEFAULT_INTEGRATIONS.osm, enabled: true, endpoints: ["https://example.test/api/interpreter"] });
    expect(await screen.findByText("OSM 检查结果")).toBeInTheDocument();
  });

  it("loads legacy OSM preferences with bounded defaults and saves edited limits without starting a download", async () => {
    mocks.getConfig.mockResolvedValue({ integrations: {
      ...DEFAULT_INTEGRATIONS,
      osm: { enabled: true, endpoints: [], nominatim_url: "", proxy_url: "", timeout_seconds: 90, max_response_mb: 100 },
    } });
    render(<IntegrationsConfigWorkspace />);
    await waitFor(() => expect(screen.getByLabelText("响应等待/停滞超时（秒）")).toHaveValue(90));
    expect(screen.getByLabelText("OSM 总时限（秒）")).toHaveValue(600);
    expect(screen.getByLabelText("最多节点尝试数")).toHaveValue(3);
    expect(screen.getByText(/持续收到数据时继续下载，但整次获取仍受总时限约束/)).toBeInTheDocument();
    expect(screen.getByText(/启用后可按任务直接下载/)).toBeInTheDocument();
    expect(screen.queryByText(/下载前会确认一次/)).not.toBeInTheDocument();
    expect(mocks.batchSetConfig).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("OSM 总时限（秒）"), { target: { value: "1800" } });
    fireEvent.change(screen.getByLabelText("最多节点尝试数"), { target: { value: "5" } });
    fireEvent.click(screen.getByRole("button", { name: "保存在线数据配置" }));
    await waitFor(() => expect(mocks.batchSetConfig).toHaveBeenCalled());
    expect(mocks.batchSetConfig.mock.calls[0][0][0].value.osm).toMatchObject({ enabled: true, timeout_seconds: 90, max_response_mb: 100, total_timeout_seconds: 1800, max_attempts: 5 });
    expect(mocks.checkIntegration).not.toHaveBeenCalled();
  });

});
