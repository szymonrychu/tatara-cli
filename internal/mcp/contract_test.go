package mcp

import "testing"

func TestCheckContractVersion_Match(t *testing.T) {
	if err := CheckContractVersion("4"); err != nil {
		t.Fatalf("matching version must be accepted, got %v", err)
	}
}

func TestCheckContractVersion_UnsetIsAllowed(t *testing.T) {
	if err := CheckContractVersion(""); err != nil {
		t.Fatalf("unset TATARA_CONTRACT_VERSION must be allowed (workstation, tests), got %v", err)
	}
}

func TestCheckContractVersion_MismatchIsFatal(t *testing.T) {
	for _, got := range []string{"1", "3", "five", "4.0", " 4"} {
		if err := CheckContractVersion(got); err == nil {
			t.Fatalf("TATARA_CONTRACT_VERSION=%q must be refused", got)
		}
	}
}

func TestContractVersionIsFour(t *testing.T) {
	if ContractVersion != 4 {
		t.Fatalf("ContractVersion = %d, want 4 (must match tatara-operator and tatara-claude-code-wrapper)", ContractVersion)
	}
}
