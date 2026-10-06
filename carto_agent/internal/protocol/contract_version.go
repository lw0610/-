package protocol

const ContractVersionV1 = "v1"

var supportedContractVersions = [...]string{ContractVersionV1}

func SupportedContractVersions() []string {
	return append([]string(nil), supportedContractVersions[:]...)
}
