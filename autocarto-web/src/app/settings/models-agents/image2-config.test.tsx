import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import Image2ConfigWorkspace from "@/components/settings/Image2ConfigWorkspace";

const { getImage2ConfigMock, saveImage2ConfigMock, testImage2ConnectionMock } =
  vi.hoisted(() => ({
    getImage2ConfigMock: vi.fn(),
    saveImage2ConfigMock: vi.fn(),
    testImage2ConnectionMock: vi.fn(),
  }));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}));

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    default: {
      getImage2Config: getImage2ConfigMock,
      saveImage2Config: saveImage2ConfigMock,
      testImage2Connection: testImage2ConnectionMock,
    },
  };
});

describe("Image2ConfigWorkspace", () => {
  beforeEach(() => {
    getImage2ConfigMock.mockReset();
    saveImage2ConfigMock.mockReset();
    testImage2ConnectionMock.mockReset();
    let storedKey = "sk-existing";
    let storedBaseUrl = "https://api.openai.com/v1";
    getImage2ConfigMock.mockImplementation(async () => ({
      enabled: true,
      model: "gpt-image-2",
      base_url: storedBaseUrl,
      api_key: storedKey,
      has_api_key: storedKey !== "",
    }));
    saveImage2ConfigMock.mockImplementation(
      async (payload: { api_key: string; base_url: string }) => {
        storedKey = payload.api_key;
        storedBaseUrl = payload.base_url;
        return {
          scope: "image2",
          persisted: true,
          required_action: "none",
        };
      },
    );
    testImage2ConnectionMock.mockResolvedValue({
      status: "ok",
      model: "gpt-image-2",
    });
  });

  it("shows only the approved compact fields", async () => {
    render(<Image2ConfigWorkspace />);

    expect(await screen.findByDisplayValue("gpt-image-2")).toBeInTheDocument();
    expect(
      screen.getByDisplayValue("https://api.openai.com/v1"),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Image 2 服务地址")).not.toHaveAttribute(
      "readonly",
    );
    expect(screen.getByText("密钥由现有本机密钥机制加密保存")).toBeInTheDocument();
    expect(screen.queryByText("对话模型不变")).not.toBeInTheDocument();
    expect(screen.queryByText(/不参与日常对话/)).not.toBeInTheDocument();
    expect(screen.queryByText("运行关系")).not.toBeInTheDocument();
  });

  it("saves and tests the same dedicated Image 2 API key and service URL", async () => {
    const user = userEvent.setup();
    render(<Image2ConfigWorkspace />);

    const keyInput = await screen.findByLabelText("OpenAI API Key");
    const baseUrlInput = screen.getByLabelText("Image 2 服务地址");
    await user.clear(keyInput);
    await user.type(keyInput, "sk-new-image2");
    await user.clear(baseUrlInput);
    await user.type(baseUrlInput, " https://image2.example.com/v1/ ");
    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => {
      expect(saveImage2ConfigMock).toHaveBeenCalledWith({
        enabled: true,
        base_url: "https://image2.example.com/v1/",
        api_key: "sk-new-image2",
      });
    });

    await user.click(
      screen.getByRole("button", { name: "测试 Image 2 连接" }),
    );
    await waitFor(() => {
      expect(testImage2ConnectionMock).toHaveBeenCalledWith({
        base_url: "https://image2.example.com/v1/",
        api_key: "sk-new-image2",
      });
    });
  });

  it("allows saving without an API key and only blocks connection testing", async () => {
    const user = userEvent.setup();
    render(<Image2ConfigWorkspace />);

    const keyInput = await screen.findByLabelText("OpenAI API Key");
    expect(screen.getByText("选填")).toBeInTheDocument();

    await user.clear(keyInput);
    expect(
      screen.getByRole("button", { name: "测试 Image 2 连接" }),
    ).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => {
      expect(saveImage2ConfigMock).toHaveBeenCalledWith({
        enabled: true,
        base_url: "https://api.openai.com/v1",
        api_key: "",
      });
    });
  });
});
