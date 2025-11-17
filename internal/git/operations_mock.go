package git

import "fmt"

// MockGitOps is a mock implementation of Operations for testing.
type MockGitOps struct {
	CurrentBranch  string
	AncestorBranch string
	Branches       []string
	RemoteURL      string
	WorktreeRoot   string
	BranchesError  error
}

// NewMockGitOps creates a mock with sensible defaults.
func NewMockGitOps() *MockGitOps {
	return &MockGitOps{
		CurrentBranch:  "main",
		AncestorBranch: "",
		Branches:       []string{"* main"},
		RemoteURL:      "https://github.com/user/repo.git",
		WorktreeRoot:   "/tmp/test-repo",
		BranchesError:  nil,
	}
}

func (m *MockGitOps) GetCurrentBranch(projectPath string) string {
	return m.CurrentBranch
}

func (m *MockGitOps) FindAncestorBranch(projectPath, currentBranch string) string {
	return m.AncestorBranch
}

func (m *MockGitOps) GetBranches(projectPath string) ([]string, error) {
	if m.BranchesError != nil {
		return nil, m.BranchesError
	}
	return m.Branches, nil
}

func (m *MockGitOps) GetRemoteURL(projectPath string) string {
	return m.RemoteURL
}

func (m *MockGitOps) GetWorktreeRoot(projectPath string) string {
	return m.WorktreeRoot
}

// BranchScenario provides common git scenarios for testing.
type BranchScenario struct {
	Name        string
	CurrentBranch string
	AncestorBranch string
	Branches []string
}

// CommonScenarios returns pre-configured mock scenarios.
func CommonScenarios() map[string]BranchScenario {
	return map[string]BranchScenario{
		"main_branch": {
			Name:           "Main branch",
			CurrentBranch:  "main",
			AncestorBranch: "main",
			Branches:       []string{"* main"},
		},
		"feature_branch": {
			Name:           "Feature branch from main",
			CurrentBranch:  "feature/auth",
			AncestorBranch: "main",
			Branches:       []string{"  main", "* feature/auth"},
		},
		"detached_head": {
			Name:           "Detached HEAD",
			CurrentBranch:  "detached-abc1234",
			AncestorBranch: "",
			Branches:       []string{"* (HEAD detached at abc1234)", "  main"},
		},
		"no_git": {
			Name:           "Not a git repository",
			CurrentBranch:  "unknown",
			AncestorBranch: "",
			Branches:       []string{},
		},
	}
}

// ApplyScenario configures a MockGitOps with a predefined scenario.
func (m *MockGitOps) ApplyScenario(scenario BranchScenario) {
	m.CurrentBranch = scenario.CurrentBranch
	m.AncestorBranch = scenario.AncestorBranch
	m.Branches = scenario.Branches
}

// String returns a human-readable representation of the mock state.
func (m *MockGitOps) String() string {
	return fmt.Sprintf("MockGitOps{branch=%s, ancestor=%s, remote=%s}",
		m.CurrentBranch, m.AncestorBranch, m.RemoteURL)
}
