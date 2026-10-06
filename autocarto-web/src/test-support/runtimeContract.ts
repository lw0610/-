import { HTTP_CONTROL_CONTRACT_VERSION } from "@/lib/api";

export const LEGACY_RUNTIME_CONTRACT_VERSION = "1";
export const LEGACY_DISCOVERY_DESCRIPTOR_VERSION = LEGACY_RUNTIME_CONTRACT_VERSION;
export const SUPPORTED_HTTP_CONTROL_CONTRACT_VERSIONS = Object.freeze([
  HTTP_CONTROL_CONTRACT_VERSION,
]);

export function buildUnsupportedControlContractData(
  requestedContractVersion = LEGACY_RUNTIME_CONTRACT_VERSION,
) {
  return {
    contract_version: HTTP_CONTROL_CONTRACT_VERSION,
    requested_contract_version: requestedContractVersion,
    supported_contract_versions: [...SUPPORTED_HTTP_CONTROL_CONTRACT_VERSIONS],
  };
}

export function buildLegacyDescriptorUnsupportedStartupError(
  version = LEGACY_DISCOVERY_DESCRIPTOR_VERSION,
) {
  return `启动后端失败: discovery descriptor contract version 不受支持: ${version}`;
}
