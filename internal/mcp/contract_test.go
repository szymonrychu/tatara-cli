package mcp

import "testing"

func TestCheckContractVersion_Match(t *testing.T) {
	if err := CheckContractVersion("2"); err != nil {
		t.Fatalf("matching version must be accepted, got %v", err)
	}
}

func TestCheckContractVersion_UnsetIsAllowed(t *testing.T) {
	if err := CheckContractVersion(""); err != nil {
		t.Fatalf("unset TATARA_CONTRACT_VERSION must be allowed (workstation, tests), got %v", err)
	}
}

func TestCheckContractVersion_MismatchIsFatal(t *testing.T) {
	for _, got := range []string{"1", "3", "two", "2.0", " 2"} {
		if err := CheckContractVersion(got); err == nil {
			t.Fatalf("TATARA_CONTRACT_VERSION=%q must be refused", got)
		}
	}
}

func TestContractVersion_IsTwo(t *testing.T) {
	if ContractVersion != 2 {
		t.Fatalf("ContractVersion must be 2 (contract G.10), got %d", ContractVersion)
	}
}
