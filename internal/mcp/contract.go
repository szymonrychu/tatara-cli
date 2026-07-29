package mcp

import (
	"fmt"
	"strconv"
)

// ContractVersion is the cross-repo agent contract this binary implements. It
// is bumped in the same release that ships a breaking tool surface. The
// operator injects TATARA_CONTRACT_VERSION into every agent pod; a mismatch
// means the pod is running an image from a different release train than the
// operator that spawned it, and every tool call it makes will 404. Contract
// G.10.
const ContractVersion = 3

// CheckContractVersion compares the injected TATARA_CONTRACT_VERSION against
// the compiled ContractVersion. An empty value is allowed: a workstation or a
// unit test has no operator to be skewed against. Any other value that is not
// exactly ContractVersion is refused, and the caller must exit non-zero rather
// than serve a tool surface the operator cannot talk to.
func CheckContractVersion(got string) error {
	if got == "" {
		return nil
	}
	n, err := strconv.Atoi(got)
	if err != nil {
		return fmt.Errorf("TATARA_CONTRACT_VERSION=%q is not an integer (this cli implements %d)", got, ContractVersion)
	}
	if n != ContractVersion {
		return fmt.Errorf("agent contract mismatch: the operator injected TATARA_CONTRACT_VERSION=%d, this cli implements %d - the agent image and the operator are from different releases", n, ContractVersion)
	}
	return nil
}
